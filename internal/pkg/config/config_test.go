package config

import "testing"

func TestConfig_Load(t *testing.T) {
	type args struct {
		path string
	}
	tests := []struct {
		name    string
		c       *config
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
		{"name", new(config), args{"../../../sample.conf"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.c.Load(tt.args.path); (err != nil) != tt.wantErr {
				t.Errorf("Config.Load() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
