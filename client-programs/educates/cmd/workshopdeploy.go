/*
Copyright © 2022 The Educates Authors.
*/
package cmd

import (
	"context"
	"encoding/json"
	"math/rand"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/vmware-tanzu-labs/educates-training-platform/client-programs/educates/pkg/cluster"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type WorkshopDeployOptions struct {
	Name       string
	Path       string
	Kubeconfig string
	Portal     string
	Capacity   uint
	Reserved   uint
	Initial    uint
	Expires    string
	Overtime   string
	Deadline   string
	Orphaned   string
}

func (o *WorkshopDeployOptions) Run() error {
	var err error

	var path = o.Path

	// Ensure have portal name.

	if o.Portal == "" {
		o.Portal = "educates-cli"
	}

	// If path not provided assume the current working directory. When loading
	// the workshop will then expect the workshop definition to reside in the
	// resources/workshop.yaml file under the directory, the same as if a
	// directory path was provided explicitly.

	if path == "" {
		path = "."
	}

	// Load the workshop definition. The path can be a HTTP/HTTPS URL for a
	// local file system path for a directory or file.

	var workshop *unstructured.Unstructured

	if workshop, err = loadWorkshopDefinition(o.Name, path, o.Portal); err != nil {
		return err
	}

	clusterConfig := cluster.NewClusterConfig(o.Kubeconfig)

	dynamicClient, err := clusterConfig.GetDynamicClient()

	if err != nil {
		return errors.Wrapf(err, "unable to create Kubernetes client")
	}

	// Update the workshop resource in the Kubernetes cluster.

	err = updateWorkshopResource(dynamicClient, workshop)

	if err != nil {
		return err
	}

	// Update the training portal, creating it if necessary.

	err = deployWorkshopResource(dynamicClient, workshop, o.Portal, o.Capacity, o.Reserved, o.Initial, o.Expires, o.Overtime, o.Deadline, o.Orphaned)

	if err != nil {
		return err
	}

	return nil
}

func NewWorkshopDeployCmd() *cobra.Command {
	var o WorkshopDeployOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "deploy-workshop",
		Short: "Deploy workshop to Kubernetes",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
	}

	c.Flags().StringVarP(
		&o.Name,
		"name",
		"n",
		"",
		"name to be used for the workshop definition, generated if not set",
	)
	c.Flags().StringVarP(
		&o.Path,
		"file",
		"f",
		".",
		"path to local workshop directory, definition file, or URL for workshop definition file",
	)
	c.Flags().StringVar(
		&o.Kubeconfig,
		"kubeconfig",
		"",
		"kubeconfig file to use instead of $KUBECONFIG or $HOME/.kube/config",
	)
	c.Flags().StringVarP(
		&o.Portal,
		"portal",
		"p",
		"educates-cli",
		"name to be used for training portal and workshop name prefixes",
	)
	c.Flags().UintVar(
		&o.Capacity,
		"capacity",
		1,
		"maximum number of current sessions for the workshop",
	)
	c.Flags().UintVar(
		&o.Reserved,
		"reserved",
		0,
		"number of workshop sessions to maintain ready in reserve",
	)
	c.Flags().UintVar(
		&o.Initial,
		"initial",
		0,
		"number of workshop sessions to create when first deployed",
	)
	c.Flags().StringVar(
		&o.Expires,
		"expires",
		"",
		"time duration before the workshop is expired",
	)
	c.Flags().StringVar(
		&o.Overtime,
		"overtime",
		"",
		"time extension allowed for the workshop",
	)
	c.Flags().StringVar(
		&o.Deadline,
		"deadline",
		"",
		"maximum time duration allowed for the workshop",
	)
	c.Flags().StringVar(
		&o.Orphaned,
		"orphaned",
		"5m",
		"allowed inactive time before workshop is terminated",
	)

	return c
}

var trainingPortalResource = schema.GroupVersionResource{Group: "training.educates.dev", Version: "v1beta1", Resource: "trainingportals"}

func deployWorkshopResource(client dynamic.Interface, workshop *unstructured.Unstructured, portal string, capacity uint, reserved uint, initial uint, expires string, overtime string, deadline string, orphaned string) error {
	trainingPortalClient := client.Resource(trainingPortalResource)

	trainingPortal, err := trainingPortalClient.Get(context.TODO(), portal, metav1.GetOptions{})

	var trainingPortalExists = true

	if k8serrors.IsNotFound(err) {
		trainingPortalExists = false

		trainingPortal = &unstructured.Unstructured{}

		trainingPortal.SetUnstructuredContent(map[string]interface{}{
			"apiVersion": "training.educates.dev/v1beta1",
			"kind":       "TrainingPortal",
			"metadata": map[string]interface{}{
				"name": portal,
			},
			"spec": map[string]interface{}{
				"portal": map[string]interface{}{
					"password": randomPassword(12),
					"registration": struct {
						Type string `json:"type"`
					}{
						Type: "anonymous",
					},
					"updates": struct {
						Workshop bool `json:"workshop"`
					}{
						Workshop: true,
					},
					"sessions": struct {
						Maximum int64 `json:"maximum"`
					}{
						Maximum: 1,
					},
					"workshop": map[string]interface{}{
						"defaults": struct {
							Reserved int `json:"reserved"`
						}{
							Reserved: 0,
						},
					},
				},
				"workshops": []interface{}{},
			},
		})
	}

	var propertyExists bool

	var sessionsMaximum int64 = 1

	if trainingPortalExists {
		sessionsMaximum, propertyExists, err = unstructured.NestedInt64(trainingPortal.Object, "spec", "portal", "sessions", "maximum")

		if err == nil && propertyExists {
			if sessionsMaximum >= 0 && uint(sessionsMaximum) < capacity {
				capacity = uint(sessionsMaximum)
			}
		}
	} else {
		capacity = 1
	}

	if reserved > capacity {
		reserved = capacity
	}

	if initial > uint(sessionsMaximum) {
		initial = uint(sessionsMaximum)
	}

	workshops, _, err := unstructured.NestedSlice(trainingPortal.Object, "spec", "workshops")

	if err != nil {
		return errors.Wrap(err, "unable to retrieve workshops from training portal")
	}

	var updatedWorkshops []interface{}

	if expires == "" {
		duration, propertyExists, err := unstructured.NestedString(workshop.Object, "spec", "duration")

		if err != nil || !propertyExists {
			expires = "60m"
		} else {
			expires = duration
		}
	}

	var foundWorkshop = false

	for _, item := range workshops {
		object := item.(map[string]interface{})

		updatedWorkshops = append(updatedWorkshops, object)

		if object["name"] == workshop.GetName() {
			foundWorkshop = true

			object["capacity"] = int64(capacity)
			object["reserved"] = int64(reserved)
			object["initial"] = int64(initial)
			object["expires"] = expires
			object["overtime"] = overtime
			object["deadline"] = deadline
			object["orphaned"] = orphaned
		}
	}

	type WorkshopDetails struct {
		Name     string `json:"name"`
		Capacity int64  `json:"capacity"`
		Initial  int64  `json:"initial"`
		Reserved int64  `json:"reserved"`
		Expires  string `json:"expires,omitempty"`
		Overtime string `json:"overtime,omitempty"`
		Deadline string `json:"deadline,omitempty"`
		Orphaned string `json:"orphaned,omitempty"`
	}

	if !foundWorkshop {
		workshopDetails := WorkshopDetails{
			Name:     workshop.GetName(),
			Capacity: int64(capacity),
			Initial:  int64(initial),
			Reserved: int64(reserved),
			Expires:  expires,
			Overtime: overtime,
			Deadline: deadline,
			Orphaned: orphaned,
		}

		var workshopDetailsMap map[string]interface{}

		data, _ := json.Marshal(workshopDetails)
		json.Unmarshal(data, &workshopDetailsMap)

		updatedWorkshops = append(updatedWorkshops, workshopDetailsMap)
	}

	unstructured.SetNestedSlice(trainingPortal.Object, updatedWorkshops, "spec", "workshops")

	if trainingPortalExists {
		_, err = trainingPortalClient.Update(context.TODO(), trainingPortal, metav1.UpdateOptions{FieldManager: "educates-cli"})
	} else {
		_, err = trainingPortalClient.Create(context.TODO(), trainingPortal, metav1.CreateOptions{FieldManager: "educates-cli"})
	}

	if err != nil {
		return errors.Wrapf(err, "unable to update training portal %q in cluster", portal)
	}

	return nil
}

func randomPassword(length int) string {
	rand.Seed(time.Now().UnixNano())

	chars := []rune("!#%+23456789:=?@ABCDEFGHJKLMNPRSTUVWXYZabcdefghijkmnopqrstuvwxyz")

	var b strings.Builder

	for i := 0; i < length; i++ {
		b.WriteRune(chars[rand.Intn(len(chars))])
	}
	return b.String()
}
