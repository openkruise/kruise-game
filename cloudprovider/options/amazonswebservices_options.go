package options

// https://docs.aws.amazon.com/elasticloadbalancing/latest/network/load-balancer-limits.html
// Listeners per Network Load Balancer is 50
const maxPortRange = 50

type AmazonsWebServicesOptions struct {
	Enable     bool          `toml:"enable"`
	NLBOptions AWSNLBOptions `toml:"nlb"`
}

type AWSNLBOptions struct {
	MaxPort int32 `toml:"max_port"`
	MinPort int32 `toml:"min_port"`
}

func (ao AmazonsWebServicesOptions) Valid() bool {
	nlbOptions := ao.NLBOptions
	if nlbOptions.MaxPort-nlbOptions.MinPort+1 > maxPortRange {
		return false
	}

	if nlbOptions.MinPort < 1 || nlbOptions.MaxPort > 65535 {
		return false
	}
	return true
}

func (ao AmazonsWebServicesOptions) Enabled() bool {
	return ao.Enable
}
