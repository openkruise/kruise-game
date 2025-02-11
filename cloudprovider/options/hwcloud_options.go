package options

type HwCloudOptions struct {
	Enable     bool       `toml:"enable"`
	ELBOptions SLBOptions `toml:"elb"`
}

type ELBOptions struct {
	MaxPort    int32   `toml:"max_port"`
	MinPort    int32   `toml:"min_port"`
	BlockPorts []int32 `toml:"block_ports"`
}

func (o HwCloudOptions) Valid() bool {
	// SLB valid
	slbOptions := o.ELBOptions
	for _, blockPort := range slbOptions.BlockPorts {
		if blockPort >= slbOptions.MaxPort || blockPort <= slbOptions.MinPort {
			return false
		}
	}
	if int(slbOptions.MaxPort-slbOptions.MinPort)-len(slbOptions.BlockPorts) >= 200 {
		return false
	}
	if slbOptions.MinPort <= 0 {
		return false
	}
	return true
}

func (o HwCloudOptions) Enabled() bool {
	return o.Enable
}
