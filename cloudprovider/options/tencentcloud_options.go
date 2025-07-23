package options

type TencentCloudOptions struct {
	Enable bool `toml:"enable"`
}

func (o TencentCloudOptions) Enabled() bool {
	return o.Enable
}

func (o TencentCloudOptions) Valid() bool {
	return true
}
