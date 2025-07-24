package cloudprovider

import (
	"io"
	"os"
	"reflect"
	"testing"

	"github.com/openkruise/kruise-game/cloudprovider/options"
)

func TestParse(t *testing.T) {
	tests := []struct {
		fileString   string
		kubernetes   options.KubernetesOptions
		alibabacloud options.AlibabaCloudOptions
	}{
		{
			fileString: `
[kubernetes]
enable = true

	[kubernetes.hostPort]
	max_port = 9000
	min_port = 8000

[alibabacloud]
enable = true
`,
			kubernetes: options.KubernetesOptions{
				Enable: true,
				HostPort: options.HostPortOptions{
					MaxPort: 9000,
					MinPort: 8000,
				},
			},
			alibabacloud: options.AlibabaCloudOptions{
				Enable: true,
			},
		},
	}

	for _, test := range tests {
		tempFile := "config"
		file, err := os.Create(tempFile)
		if err != nil {
			t.Errorf("open file failed, because of %s", err.Error())
		}
		_, err = io.WriteString(file, test.fileString)
		if err != nil {
			t.Errorf("write file failed, because of %s", err.Error())
		}
		err = file.Close()
		if err != nil {
			t.Errorf("close file failed, because of %s", err.Error())
		}

		cf := ConfigFile{
			Path: tempFile,
		}
		cloudProviderConfig := cf.Parse()

		if !reflect.DeepEqual(cloudProviderConfig.AlibabaCloudOptions, test.alibabacloud) {
			t.Errorf("expect AlibabaCloudOptions: %v, but got %v", test.alibabacloud, cloudProviderConfig.AlibabaCloudOptions)
		}
		if !reflect.DeepEqual(cloudProviderConfig.KubernetesOptions, test.kubernetes) {
			t.Errorf("expect KubernetesOptions: %v, but got %v", test.kubernetes, cloudProviderConfig.KubernetesOptions)
		}
		os.Remove(tempFile)
	}
}
