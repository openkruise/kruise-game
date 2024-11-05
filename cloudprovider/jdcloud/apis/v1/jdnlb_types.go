package v1

const (
	LbType_NLB = "nlb"
)

type JdNLBElasticIp struct {
	ElasticIpId string `json:"elasticIpId"`
}

type JdNLBAlgorithm string

const (
	JdNLBDefaultConnIdleTime int            = 600
	JdNLBAlgorithmRoundRobin JdNLBAlgorithm = "RoundRobin"
	JdNLBAlgorithmLeastConn  JdNLBAlgorithm = "LeastConn"
	JdNLBAlgorithmIpHash     JdNLBAlgorithm = "IpHash"
)

type JdNLBListenerBackend struct {
	ProxyProtocol bool           `json:"proxyProtocol"`
	Algorithm     JdNLBAlgorithm `json:"algorithm"`
}

type JdNLBListener struct {
	Protocol                  string                `json:"protocol"`
	ConnectionIdleTimeSeconds int                   `json:"connectionIdleTimeSeconds"`
	Backend                   *JdNLBListenerBackend `json:"backend"`
}

type JdNLB struct {
	Version          string           `json:"version"`
	LoadBalancerId   string           `json:"loadBalancerId"`
	LoadBalancerType string           `json:"loadBalancerType"`
	Internal         bool             `json:"internal"`
	Listeners        []*JdNLBListener `json:"listeners"`
}
