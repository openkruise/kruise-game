package options

type JdCloudOptions struct {
	Enable     bool         `toml:"enable"`
	NLBOptions JdNLBOptions `toml:"nlb"`
}

type JdNLBOptions struct {
	MaxPort int32 `toml:"max_port"`
	MinPort int32 `toml:"min_port"`
}

func (v JdCloudOptions) Valid() bool {
	nlbOptions := v.NLBOptions

	if nlbOptions.MaxPort > 65535 {
		return false
	}

	if nlbOptions.MinPort < 1 {
		return false
	}
	return true
}

func (v JdCloudOptions) Enabled() bool {
	return v.Enable
}
