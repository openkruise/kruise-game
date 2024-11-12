package options

type VolcengineOptions struct {
	Enable     bool       `toml:"enable"`
	CLBOptions CLBOptions `toml:"clb"`
}

type CLBOptions struct {
	MaxPort    int32   `toml:"max_port"`
	MinPort    int32   `toml:"min_port"`
	BlockPorts []int32 `toml:"block_ports"`
}

func (v VolcengineOptions) Valid() bool {
	clbOptions := v.CLBOptions

	for _, blockPort := range clbOptions.BlockPorts {
		if blockPort >= clbOptions.MaxPort || blockPort < clbOptions.MinPort {
			return false
		}
	}

	if clbOptions.MaxPort > 65535 {
		return false
	}

	if clbOptions.MinPort < 1 {
		return false
	}
	return true
}

func (v VolcengineOptions) Enabled() bool {
	return v.Enable
}
