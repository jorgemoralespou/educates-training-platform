package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/explain"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1/schemas"
)

// explainableKinds maps configuration kind names, and short aliases, to
// their embedded JSON schema. Selected via the --kind flag; defaults to
// EducatesLocalConfig when --kind is not provided.
var explainableKinds = map[string][]byte{
	"educateslocalconfig":  schemas.EducatesLocalConfig,
	"local":                schemas.EducatesLocalConfig,
	"educatesgkeconfig":    schemas.EducatesGKEConfig,
	"gke":                  schemas.EducatesGKEConfig,
	"educateseksconfig":    schemas.EducatesEKSConfig,
	"eks":                  schemas.EducatesEKSConfig,
	"educatesinlineconfig": schemas.EducatesInlineConfig,
	"inline":               schemas.EducatesInlineConfig,
	"educatesconfig":       schemas.EducatesConfig,
	"escape":               schemas.EducatesConfig,
}

type LocalConfigExplainOptions struct {
	Kind string
}

func (p *ProjectInfo) NewLocalConfigExplainCmd() *cobra.Command {
	var o LocalConfigExplainOptions

	c := &cobra.Command{
		Args:  cobra.MaximumNArgs(1),
		Use:   "explain [PATH]",
		Short: "Describe configuration fields, like 'kubectl explain'",
		Long: `Describe the fields of an Educates CLI configuration kind from its
embedded JSON schema, in the style of 'kubectl explain'.

PATH is a dotted field path, for example 'ingress' or 'ingress.insecure'.
With no argument the top-level fields are listed. The kind is chosen with
--kind, defaulting to EducatesLocalConfig. No cluster or configuration
file is needed; the descriptions come from the schema.

--kind accepts a kind name or a short alias:
  EducatesLocalConfig (local), EducatesGKEConfig (gke),
  EducatesEKSConfig (eks), EducatesInlineConfig (inline),
  EducatesConfig (escape).`,
		Example: `  educates local config explain
  educates local config explain ingress.insecure
  educates local config explain gcp.project --kind gke
  educates local config explain policyEnforcement --kind inline
  educates local config explain educatesClusterConfig --kind escape`,
		RunE: func(cmd *cobra.Command, args []string) error {
			schema, err := explainSchemaForKind(o.Kind)
			if err != nil {
				return err
			}
			path := ""
			if len(args) == 1 {
				path = args[0]
			}
			out, err := explain.Explain(schema, path)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), out)
			return nil
		},
	}
	c.Flags().StringVar(&o.Kind, "kind", "",
		"configuration kind to explain (default EducatesLocalConfig); a kind name or alias local|gke|eks|inline|escape")
	return c
}

// explainSchemaForKind resolves the --kind flag to an embedded schema.
// An empty value selects EducatesLocalConfig; an unrecognised value is an
// error that lists the valid kinds.
func explainSchemaForKind(kind string) ([]byte, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return schemas.EducatesLocalConfig, nil
	}
	if schema, ok := explainableKinds[strings.ToLower(kind)]; ok {
		return schema, nil
	}
	return nil, fmt.Errorf("unknown --kind %q; valid kinds: EducatesLocalConfig, EducatesGKEConfig, EducatesEKSConfig, EducatesInlineConfig, EducatesConfig (aliases: local, gke, eks, inline, escape)", kind)
}
