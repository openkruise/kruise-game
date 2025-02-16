package options

type AlibabaCloudOptions struct {
	Enable     bool       `toml:"enable"`
	SLBOptions SLBOptions `toml:"slb"`
	NLBOptions NLBOptions `toml:"nlb"`
}

type SLBOptions struct {
	MaxPort    int32   `toml:"max_port"`
	MinPort    int32   `toml:"min_port"`
	BlockPorts []int32 `toml:"block_ports"`
}

type NLBOptions struct {
	MaxPort    int32   `toml:"max_port"`
	MinPort    int32   `toml:"min_port"`
	BlockPorts []int32 `toml:"block_ports"`
}

func (o AlibabaCloudOptions) Valid() bool {
	// SLB valid
	slbOptions := o.SLBOptions
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
	// NLB valid
	nlbOptions := o.NLBOptions
	for _, blockPort := range nlbOptions.BlockPorts {
		if blockPort >= nlbOptions.MaxPort || blockPort <= nlbOptions.MinPort {
			return false
		}
	}
	if int(nlbOptions.MaxPort-nlbOptions.MinPort)-len(nlbOptions.BlockPorts) >= 1000 {
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
