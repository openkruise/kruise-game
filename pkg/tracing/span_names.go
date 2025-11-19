package tracing

// SpanAdmissionMutatePod defines the canonical root span name for the Admission webhook
// responsible for mutating GameServer pods. Keep this in sync with the tracing spec's
// verb-object naming convention so tests and collectors stay aligned.
const SpanAdmissionMutatePod = "mutate pod admission"
