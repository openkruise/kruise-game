package options

type HwCloudOptions struct {
	Enable     bool       `toml:"enable"`
	ELBOptions ELBOptions `toml:"elb"`
}

type ELBOptions struct {
	MaxPort    int32   `toml:"max_port"`
	MinPort    int32   `toml:"min_port"`
	BlockPorts []int32 `toml:"block_ports"`
}

func (o HwCloudOptions) Valid() bool {
	elbOptions := o.ELBOptions
	for _, blockPort := range elbOptions.BlockPorts {
		if blockPort >= elbOptions.MaxPort || blockPort <= elbOptions.MinPort {
			return false
		}
	}
	if int(elbOptions.MaxPort-elbOptions.MinPort)-len(elbOptions.BlockPorts) > 200 {
		return false
	}
	if elbOptions.MinPort <= 0 {
		return false
	}
	return true
}

func (o HwCloudOptions) Enabled() bool {
	return o.Enable
}
