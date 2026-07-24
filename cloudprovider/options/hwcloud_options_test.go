package options

import "testing"

func TestHwCloudOptionsValidRequiresMultiELBOptions(t *testing.T) {
	options := HwCloudOptions{
		ELBOptions: ELBOptions{
			MinPort: 500,
			MaxPort: 700,
		},
		CCEELBOptions: CCEELBOptions{
			ELBOptions: ELBOptions{
				MinPort: 32768,
				MaxPort: 65535,
			},
		},
	}

	if options.Valid() {
		t.Fatal("expected HwCloudOptions.Valid() to reject missing multi-elb port range")
	}
}

func TestHwCloudOptionsValidAcceptsAllELBOptions(t *testing.T) {
	options := HwCloudOptions{
		ELBOptions: ELBOptions{
			MinPort: 500,
			MaxPort: 700,
		},
		CCEELBOptions: CCEELBOptions{
			ELBOptions: ELBOptions{
				MinPort: 32768,
				MaxPort: 65535,
			},
			MultiELBOptions: ELBOptions{
				MinPort: 32700,
				MaxPort: 65535,
			},
		},
	}

	if !options.Valid() {
		t.Fatal("expected HwCloudOptions.Valid() to accept complete hwcloud port ranges")
	}
}
