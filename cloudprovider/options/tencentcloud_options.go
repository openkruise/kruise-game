package options

type TencentCloudOptions struct {
	Enable     bool                   `toml:"enable"`
	CLBOptions TencentCloudCLBOptions `toml:"clb"`
}

type TencentCloudCLBOptions struct {
	MaxPort int32 `toml:"max_port"`
	MinPort int32 `toml:"min_port"`
}

func (o TencentCloudOptions) Valid() bool {
	clbOptions := o.CLBOptions

	if clbOptions.MaxPort > 65535 {
		return false
	}

	if clbOptions.MinPort < 1 {
		return false
	}

	return true
}

func (o TencentCloudOptions) Enabled() bool {
	return o.Enable
}
