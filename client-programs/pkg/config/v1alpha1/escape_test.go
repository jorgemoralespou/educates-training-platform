package v1alpha1

import "testing"

func TestEducatesConfigIngressDomain(t *testing.T) {
	cases := []struct {
		name string
		ecc  map[string]interface{}
		want string
	}{
		{
			name: "managed",
			ecc: map[string]interface{}{
				"mode":    "Managed",
				"ingress": map[string]interface{}{"domain": "workshops.example.test"},
			},
			want: "workshops.example.test",
		},
		{
			name: "mode left to the CRD",
			ecc: map[string]interface{}{
				"ingress": map[string]interface{}{"domain": "workshops.example.test"},
			},
			want: "workshops.example.test",
		},
		{
			name: "inline",
			ecc: map[string]interface{}{
				"mode": "Inline",
				"inline": map[string]interface{}{
					"ingress": map[string]interface{}{"domain": "inline.example.test"},
				},
			},
			want: "inline.example.test",
		},
		{
			name: "yaml.v2 shaped maps",
			ecc: map[string]interface{}{
				"ingress": map[interface{}]interface{}{"domain": "workshops.example.test"},
			},
			want: "workshops.example.test",
		},
		{
			name: "no domain",
			ecc:  map[string]interface{}{"mode": "Managed"},
		},
		{
			name: "no educatesClusterConfig",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &EducatesConfig{EducatesClusterConfig: tc.ecc}
			if got := c.IngressDomain(); got != tc.want {
				t.Errorf("IngressDomain() = %q, want %q", got, tc.want)
			}
		})
	}
}
