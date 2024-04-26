package options

type AlibabaCloudOptions struct {
	Enable     bool       `toml:"enable"`
	SLBOptions SLBOptions `toml:"slb"`
	NLBOptions NLBOptions `toml:"nlb"`
}

type SLBOptions struct {
	MaxPort int32 `toml:"max_port"`
	MinPort int32 `toml:"min_port"`
}

type NLBOptions struct {
	MaxPort int32 `toml:"max_port"`
	MinPort int32 `toml:"min_port"`
}

func (o AlibabaCloudOptions) Valid() bool {
	// SLB valid
	slbOptions := o.SLBOptions
	if slbOptions.MaxPort-slbOptions.MinPort != 200 {
		return false
	}
	if slbOptions.MinPort <= 0 {
		return false
	}
	// NLB valid
	nlbOptions := o.NLBOptions
	if nlbOptions.MaxPort-nlbOptions.MinPort != 500 {
		return false
	}
	if nlbOptions.MinPort <= 0 {
		return false
	}
	return true
}

func (o AlibabaCloudOptions) Enabled() bool {
	return o.Enable
}
