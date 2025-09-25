package framework

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// FilterAuditLog reads the audit log file, selects entries after sinceTS
// within the given namespace, and returns up to maxLines formatted lines.
func FilterAuditLog(path string, sinceTS time.Time, namespace string, maxLines int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Allow larger lines for RequestResponse bodies
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	type objRef struct {
		Namespace   string `json:"namespace"`
		Resource    string `json:"resource"`
		APIGroup    string `json:"apiGroup"`
		Name        string `json:"name"`
		Subresource string `json:"subresource"`
	}
	type userInfo struct {
		Username string `json:"username"`
	}
	type respStatus struct {
		Code int `json:"code"`
	}
	type event struct {
		Verb                 string          `json:"verb"`
		Stage                string          `json:"stage"`
		RequestURI           string          `json:"requestURI"`
		StageTimestamp       string          `json:"stageTimestamp"`
		ObjectRef            objRef          `json:"objectRef"`
		User                 userInfo        `json:"user"`
		UserAgent            string          `json:"userAgent"`
		ResponseStatus       respStatus      `json:"responseStatus"`
		RequestReceivedStamp string          `json:"requestReceivedTimestamp"`
		RequestObject        json.RawMessage `json:"requestObject,omitempty"`
		ResponseObject       json.RawMessage `json:"responseObject,omitempty"`
	}

	// ring buffer of last matches
	lines := make([]string, 0, maxLines)
	push := func(s string) {
		if len(lines) == maxLines {
			copy(lines, lines[1:])
			lines[len(lines)-1] = s
		} else {
			lines = append(lines, s)
		}
	}

	// keep last seen flattened state per object to compute generic diffs
	lastObj := make(map[string]map[string]string)
	keyOf := func(ev event) string {
		grp := ev.ObjectRef.APIGroup
		if grp == "" {
			grp = "core"
		}
		sub := ev.ObjectRef.Subresource
		if sub != "" {
			return fmt.Sprintf("%s/%s/%s/%s", grp, ev.ObjectRef.Resource+"/"+sub, ev.ObjectRef.Namespace, ev.ObjectRef.Name)
		}
		return fmt.Sprintf("%s/%s/%s/%s", grp, ev.ObjectRef.Resource, ev.ObjectRef.Namespace, ev.ObjectRef.Name)
	}

	for scanner.Scan() {
		b := scanner.Bytes()
		var ev event
		if err := json.Unmarshal(b, &ev); err != nil {
			continue
		}
		if ev.Stage != "ResponseComplete" {
			continue
		}
		if ev.ObjectRef.Namespace != namespace {
			continue
		}
		// time filter
		if sinceTS.After(time.Time{}) {
			tsStr := ev.StageTimestamp
			if tsStr == "" {
				tsStr = ev.RequestReceivedStamp
			}
			if tsStr != "" {
				if ts, err := time.Parse(time.RFC3339Nano, tsStr); err == nil {
					if ts.Before(sinceTS) {
						continue
					}
				}
			}
		}
		// Only mutating verbs
		switch ev.Verb {
		case "update", "patch", "delete", "deletecollection":
		default:
			continue
		}

		grp := ev.ObjectRef.APIGroup
		if grp == "" {
			grp = "core"
		}
		res := ev.ObjectRef.Resource
		if ev.ObjectRef.Subresource != "" {
			res = res + "/" + ev.ObjectRef.Subresource
		}

		// Filter out particularly noisy/low-value resources (events, leases, etc.)
		if res == "events" || strings.HasSuffix(res, "/events") || strings.HasSuffix(grp, "events.k8s.io") || res == "leases" {
			continue
		}
		// Filter known high-frequency user agents (e.g., kruise pod-readiness-controller)
		if strings.Contains(strings.ToLower(ev.UserAgent), "pod-readiness") {
			continue
		}

		// Compute field changes (generic): prefer diff between ResponseObject and last snapshot;
		// if there is no prior snapshot, fall back to fields from the request body.
		var changeSuffix string
		curFlat := flattenJSON(ev.ResponseObject, 4, 200)
		objKey := keyOf(ev)
		if len(curFlat) > 0 {
			if prev, ok := lastObj[objKey]; ok {
				diff := diffFlat(prev, curFlat)
				changeSuffix = formatChanges(diff, 12)
			} else {
				// First occurrence: show intent from the request body (if available)
				reqFlat := flattenJSON(ev.RequestObject, 3, 120)
				changeSuffix = formatChanges(reqFlat, 12)
			}
			lastObj[objKey] = curFlat
		} else {
			// No response object (e.g., some delete operations): fall back to request fields
			reqFlat := flattenJSON(ev.RequestObject, 3, 120)
			changeSuffix = formatChanges(reqFlat, 12)
			if changeSuffix == "" && (ev.Verb == "delete" || ev.Verb == "deletecollection") {
				changeSuffix = " chg=deleted"
			}
		}

		// who/what/where
		push(fmt.Sprintf("%s usr=%s verb=%s %s/%s ns=%s name=%s code=%d ua=%s uri=%s%s",
			ev.StageTimestamp,
			ev.User.Username,
			ev.Verb,
			grp, res,
			ev.ObjectRef.Namespace,
			ev.ObjectRef.Name,
			ev.ResponseStatus.Code,
			trimUA(ev.UserAgent),
			ev.RequestURI,
			changeSuffix,
		))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func trimUA(ua string) string {
	if len(ua) > 120 {
		return ua[:120] + "..."
	}
	return ua
}

// flattenJSON flattens a JSON object into path->value strings, filtering noisy keys.
// depth limits recursion depth; limit caps number of entries to avoid blow-up.
func flattenJSON(raw json.RawMessage, depth int, limit int) map[string]string {
	if len(raw) == 0 || limit <= 0 {
		return nil
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	out := make(map[string]string)
	var n int
	var rec func(prefix string, x interface{}, d int)
	rec = func(prefix string, x interface{}, d int) {
		if n >= limit || d < 0 {
			return
		}
		switch vv := x.(type) {
		case map[string]interface{}:
			// stable order for determinism
			keys := make([]string, 0, len(vv))
			for k := range vv {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				val := vv[k]
				path := k
				if prefix != "" {
					path = prefix + "." + k
				}
				if noisyKey(path) {
					continue
				}
				rec(path, val, d-1)
				if n >= limit {
					return
				}
			}
		case []interface{}:
			// Summarize array values by first few entries and length
			l := len(vv)
			max := l
			if max > 3 {
				max = 3
			}
			for i := 0; i < max && n < limit; i++ {
				path := prefix + "[" + strconv.Itoa(i) + "]"
				rec(path, vv[i], d-1)
			}
			out[prefix+".#"] = strconv.Itoa(l)
			n++
		case string:
			out[prefix] = vv
			n++
		case bool:
			out[prefix] = strconv.FormatBool(vv)
			n++
		case float64:
			// JSON numbers decode to float64; render without trailing .0 when integer
			if vv == float64(int64(vv)) {
				out[prefix] = strconv.FormatInt(int64(vv), 10)
			} else {
				out[prefix] = strconv.FormatFloat(vv, 'f', -1, 64)
			}
			n++
		case nil:
			out[prefix] = "<nil>"
			n++
		default:
			// fallback to JSON string for nested objects we didn't dive into
			b, _ := json.Marshal(vv)
			s := string(b)
			if len(s) > 80 {
				s = s[:80] + "..."
			}
			out[prefix] = s
			n++
		}
	}
	rec("", v, depth)
	if len(out) == 0 {
		return nil
	}
	return out
}

// diffFlat returns paths->newValue where current differs from prev (added or changed).
func diffFlat(prev, cur map[string]string) map[string]string {
	out := make(map[string]string)
	for k, v := range cur {
		if pv, ok := prev[k]; !ok || pv != v {
			out[k] = v
		}
	}
	return out
}

// noisyKey filters out high-churn fields that add little debug value.
func noisyKey(path string) bool {
	if path == "" {
		return false
	}
	p := path
	if strings.HasPrefix(p, "metadata.") {
		if strings.HasPrefix(p, "metadata.managedFields") ||
			strings.HasPrefix(p, "metadata.resourceVersion") ||
			strings.HasPrefix(p, "metadata.uid") ||
			strings.HasPrefix(p, "metadata.creationTimestamp") {
			return true
		}
	}
	// timestamps and probe churn
	if strings.Contains(p, "lastTransitionTime") || strings.Contains(p, "lastProbeTime") {
		return true
	}
	// containerStatuses can be verbose; summarize by count
	if strings.HasPrefix(p, "status.containerStatuses") {
		return true
	}
	return false
}

// formatChanges renders key=value pairs compactly, limiting count.
func formatChanges(m map[string]string, max int) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	show := keys
	more := 0
	if len(keys) > max {
		show = keys[:max]
		more = len(keys) - max
	}
	parts := make([]string, 0, len(show))
	for _, k := range show {
		v := m[k]
		if len(v) > 100 {
			v = v[:100] + "..."
		}
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	suffix := " chg=" + strings.Join(parts, ",")
	if more > 0 {
		suffix += fmt.Sprintf(" (+%d)", more)
	}
	return suffix
}
