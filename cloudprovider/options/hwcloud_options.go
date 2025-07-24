package options

type HwCloudOptions struct {
	Enable        bool          `toml:"enable"`
	ELBOptions    ELBOptions    `toml:"elb"`
	CCEELBOptions CCEELBOptions `toml:"cce"`
}

type CCEELBOptions struct {
	ELBOptions ELBOptions `toml:"elb"`
}

type ELBOptions struct {
	MaxPort    int32   `toml:"max_port"`
	MinPort    int32   `toml:"min_port"`
	BlockPorts []int32 `toml:"block_ports"`
}

func (e ELBOptions) valid(skipPortRangeCheck bool) bool {
	for _, blockPort := range e.BlockPorts {
		if blockPort >= e.MaxPort || blockPort <= e.MinPort {
			return false
		}
	}
	// old elb plugin only allow 200 ports.
	if !skipPortRangeCheck && int(e.MaxPort-e.MinPort)-len(e.BlockPorts) > 200 {
		return false
	}
	if e.MinPort <= 0 || e.MaxPort > 65535 {
		return false
	}
	return true
}

func (o HwCloudOptions) Valid() bool {
	elbOptions := o.ELBOptions
	cceElbOptions := o.CCEELBOptions
	return elbOptions.valid(false) && cceElbOptions.ELBOptions.valid(true)
}

func (o HwCloudOptions) Enabled() bool {
	return o.Enable
}
