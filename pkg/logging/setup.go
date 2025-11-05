package logging

import (
	"flag"
	"fmt"

	"github.com/go-logr/zapr"
	gozap "go.uber.org/zap"
	gozapcore "go.uber.org/zap/zapcore"
	klog "k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	defaultLogFormat   = "console"
	defaultJSONPreset  = string(JSONPresetKibana)
	logFormatFlagName  = "log-format"
	jsonPresetFlagName = "log-json-preset"
	zapEncoderFlagName = "zap-encoder"
)

// Options centralizes all flags and zap configuration required for logger bootstrap.
type Options struct {
	ZapOptions zap.Options
	JSONConfig JSONConfig

	logFormat     string
	logJSONPreset string
}

// Result captures the final logging state after Apply runs.
type Result struct {
	Format     string
	JSONPreset JSONPreset
	Warning    string
}

// NewOptions returns Options preconfigured with controller-runtime friendly defaults.
func NewOptions() *Options {
	o := &Options{
		ZapOptions: zap.Options{
			Development: true,
		},
		logFormat:     defaultLogFormat,
		logJSONPreset: defaultJSONPreset,
	}

	o.ZapOptions.ZapOpts = append(o.ZapOptions.ZapOpts,
		gozap.AddCaller(),
		gozap.WrapCore(func(c gozapcore.Core) gozapcore.Core {
			return WrapCore(c, 2)
		}),
	)

	return o
}

// AddFlags registers logging-related flags on the provided FlagSet.
func (o *Options) AddFlags(fs *flag.FlagSet) {
	if o == nil {
		return
	}
	if fs == nil {
		fs = flag.CommandLine
	}

	fs.StringVar(&o.logFormat, logFormatFlagName, o.logFormat, "Log output format. 'console' or 'json'. Defaults to 'console'. Overrides --zap-encoder.")
	fs.StringVar(&o.logJSONPreset, jsonPresetFlagName, o.logJSONPreset, "JSON field layout preset when --log-format=json. Options: 'kibana' or 'otel'.")
	o.ZapOptions.BindFlags(fs)
}

// Apply wires controller-runtime, klog, and std loggers according to parsed flags and returns the resulting state.
func (o *Options) Apply(fs *flag.FlagSet) (Result, error) {
	if o == nil {
		return Result{}, fmt.Errorf("logging options is nil")
	}
	if fs == nil {
		fs = flag.CommandLine
	}

	preset, err := ParseJSONPreset(o.logJSONPreset)
	if err != nil {
		return Result{}, err
	}

	cfg := o.JSONConfig
	cfg.Preset = preset
	SetJSONConfig(cfg)

	logFormatChanged := false
	zapEncoderChanged := false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case logFormatFlagName:
			logFormatChanged = true
		case zapEncoderFlagName:
			zapEncoderChanged = true
		}
	})

	zapEncoderFlag := fs.Lookup(zapEncoderFlagName)
	zapEncoder := ""
	if zapEncoderFlag != nil {
		zapEncoder = zapEncoderFlag.Value.String()
	}

	finalFormat := defaultLogFormat
	if o.logFormat != "" {
		finalFormat = o.logFormat
	}

	warning := ""
	if logFormatChanged {
		finalFormat = o.logFormat
		if zapEncoderChanged && zapEncoder != o.logFormat {
			warning = fmt.Sprintf("WARNING: --log-format overrides --zap-encoder (%s vs %s)", o.logFormat, zapEncoder)
		}
	} else if zapEncoderChanged {
		finalFormat = zapEncoder
	}

	switch finalFormat {
	case "", "console":
		finalFormat = "console"
		o.ZapOptions.Encoder = nil
		setActiveJSON(false)
	case "json":
		setActiveJSON(true)
		switch preset {
		case JSONPresetOTel:
			o.ZapOptions.Encoder = NewOTelJSONEncoder()
		default:
			o.ZapOptions.Encoder = NewKibanaJSONEncoder()
		}
	default:
		return Result{}, fmt.Errorf("unsupported log-format %s", finalFormat)
	}

	logger := zap.New(zap.UseFlagOptions(&o.ZapOptions))
	cfg = currentJSONConfig()
	if kv := cfg.resourceKeyValues(); len(kv) > 0 {
		logger = logger.WithValues(kv...)
	}

	ctrl.SetLogger(logger)
	klog.SetLogger(ctrl.Log)

	if zl, ok := ctrl.Log.GetSink().(zapr.Underlier); ok {
		if u := zl.GetUnderlying(); u != nil {
			_ = gozap.ReplaceGlobals(u)
			_ = gozap.RedirectStdLog(u)
		}
	}

	return Result{
		Format:     finalFormat,
		JSONPreset: preset,
		Warning:    warning,
	}, nil
}
