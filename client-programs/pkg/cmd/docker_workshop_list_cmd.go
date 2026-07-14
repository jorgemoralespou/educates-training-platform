package cmd

import (
	"context"
	"fmt"
	"sync"

	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/moby/moby/client"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var dockerWorkshopListExample = `
  # List the workshops deployed to Docker:
  educates docker workshop list
`

func (p *ProjectInfo) NewDockerWorkshopListCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "list",
		Short:   "List workshops deployed to Docker",
		Example: dockerWorkshopListExample,
		RunE: func(_ *cobra.Command, _ []string) error {
			dockerWorkshopsManager := NewDockerWorkshopsManager()

			workshops, err := dockerWorkshopsManager.ListWorkhops()

			if err != nil {
				return errors.Wrap(err, "cannot display list of workshops")
			}

			rows := make([][]string, 0, len(workshops))
			for _, workshop := range workshops {
				rows = append(rows, []string{workshop.Name, workshop.Url, workshop.Source, workshop.Status})
			}

			fmt.Println(utils.PrintTable([]string{"NAME", "URL", "SOURCE", "STATUS"}, rows))

			return nil
		},
	}

	return c
}

type DockerWorkshopsManager struct {
	Statuses      map[string]DockerWorkshopDetails
	StatusesMutex sync.Mutex
}

func NewDockerWorkshopsManager() DockerWorkshopsManager {
	return DockerWorkshopsManager{
		Statuses:      map[string]DockerWorkshopDetails{},
		StatusesMutex: sync.Mutex{},
	}
}

type DockerWorkshopDetails struct {
	Name   string `json:"name"`
	Url    string `json:"url,omitempty"`
	Source string `json:"source,omitempty"`
	Status string `json:"status"`
}

func (m *DockerWorkshopsManager) WorkshopStatus(name string) (DockerWorkshopDetails, bool) {
	workshops, err := m.ListWorkhops()

	if err != nil {
		return DockerWorkshopDetails{}, false
	}

	for _, workshop := range workshops {
		if workshop.Name == name {
			return workshop, true
		}
	}

	return DockerWorkshopDetails{}, false
}

func (m *DockerWorkshopsManager) SetWorkshopStatus(name string, url string, source string, status string) {
	m.StatusesMutex.Lock()

	m.Statuses[name] = DockerWorkshopDetails{
		Name:   name,
		Url:    url,
		Source: source,
		Status: status,
	}

	m.StatusesMutex.Unlock()
}

func (m *DockerWorkshopsManager) ClearWorkshopStatus(name string) {
	m.StatusesMutex.Lock()

	delete(m.Statuses, name)

	m.StatusesMutex.Unlock()
}

func (m *DockerWorkshopsManager) ListWorkhops() ([]DockerWorkshopDetails, error) {
	setOfWorkshops := map[string]DockerWorkshopDetails{}
	workshopsList := []DockerWorkshopDetails{}

	ctx := context.Background()

	cli, err := docker.NewDockerClient()

	if err != nil {
		return nil, errors.Wrap(err, "unable to create docker client")
	}

	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{})

	if err != nil {
		return nil, errors.Wrap(err, "unable to list containers")
	}

	m.StatusesMutex.Lock()

	for _, details := range m.Statuses {
		if details.Status == "Starting" {
			setOfWorkshops[details.Name] = details
		}
	}

	defer m.StatusesMutex.Unlock()

	for _, container := range containers.Items {
		url, found := container.Labels["training.educates.dev/url"]
		source := container.Labels["training.educates.dev/source"]
		instance := container.Labels["training.educates.dev/session"]

		details, statusFound := m.Statuses[instance]

		status := "Running"

		if statusFound {
			status = details.Status
		}

		if found && url != "" && len(container.Names) != 0 {
			setOfWorkshops[instance] = DockerWorkshopDetails{
				Name:   instance,
				Url:    url,
				Source: source,
				Status: status,
			}
		}
	}

	for _, details := range setOfWorkshops {
		workshopsList = append(workshopsList, details)
	}

	return workshopsList, nil
}
