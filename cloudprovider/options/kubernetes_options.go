package options

type KubernetesOptions struct {
	Enable   bool            `toml:"enable"`
	HostPort HostPortOptions `toml:"hostPort"`
}

type HostPortOptions struct {
	MaxPort    int32 `toml:"max_port"`
	MinPort    int32 `toml:"min_port"`
	ShardCount int   `toml:"shard_count"` // Number of shards for port allocation (0 = auto, default 1 for backward compatibility)
}

func (o KubernetesOptions) Valid() bool {
	// HostPort valid
	slbOptions := o.HostPort
	if slbOptions.MaxPort <= slbOptions.MinPort {
		return false
	}
	if slbOptions.MinPort <= 0 {
		return false
	}
	return true
}

func (o KubernetesOptions) Enabled() bool {
	return o.Enable
}
