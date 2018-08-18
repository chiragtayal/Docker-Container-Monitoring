package main

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
)

const (
	statsTick = 10
)

type containerMonitor struct {
	monitorDb map[string]*monitor
	lock      *sync.Mutex
}

type monitor struct {
	stats   chan *types.StatsJSON
	done    chan bool
	cid     string
	stopped bool
}

var cminstance *containerMonitor
var cmonce sync.Once

func getDockerClient() (*client.Client, error) {

	return client.NewClient("unix:///var/run/docker.sock", "v1.18", nil,
		map[string]string{"User-Agent": "engine-api-cli-1.0"})
}

func getContainerMonitor() *containerMonitor {

	cmonce.Do(func() {

		log.Info("Creating Container Monitor Instance")

		cminstance = &containerMonitor{}
		cminstance.lock = &sync.Mutex{}
		cminstance.monitorDb = make(map[string]*monitor)

	})

	return cminstance
}

func New() *containerMonitor {

	return getContainerMonitor()

}

func (cm *containerMonitor) monitorRunningContainers() {

	client, err := getDockerClient()
	if err != nil {
		return
	}

	containers, err := client.ContainerList(context.Background(), types.ContainerListOptions{})
	if err != nil {
		return
	}

	for _, container := range containers {

		ms := &monitor{}
		ms.stats = make(chan *types.StatsJSON)
		ms.done = make(chan bool)
		ms.cid = container.ID

		cm.lock.Lock()
		cm.monitorDb[container.ID] = ms
		cm.lock.Unlock()

		go ms.monitor(client)
	}
}

func (ms *monitor) monitor(cli *client.Client) {

	ctx, cancel := context.WithCancel(context.Background())

	stats, err := cli.ContainerStats(ctx, ms.cid, true)
	if err != nil {
		log.Errorf("%s", err.Error())
	}

	dec := json.NewDecoder(stats.Body)
	var v *types.StatsJSON

	go func() {
		log.Info("Start Monitoring for Container : ", ms.cid)

		for {
			select {
			case <-ctx.Done():
				log.Error("Context canceled : ", ms.cid)
				stats.Body.Close()
				ms.stopped = true
				close(ms.stats)
				close(ms.done)
				cancel()
				return
			case <-ms.done:
				log.Error("End Monitoring for Container : ", ms.cid)
				stats.Body.Close()
				ms.stopped = true
				close(ms.stats)
				close(ms.done)
				cancel()
				return
			default:
				if err := dec.Decode(&v); err != nil {
					log.Error("Unable to decode stats : ", err.Error())
					ms.stopped = true
					close(ms.stats)
					close(ms.done)
					cancel()
					return
				}
				ms.stats <- v
			}
		}
	}()
}

func (cm *containerMonitor) collector() {

	ticker := time.NewTicker(time.Second * statsTick)
	go func() {
		for t := range ticker.C {
			log.Info("Collecting stats: ", t)

			for cid := range cm.monitorDb {
				ms := cm.monitorDb[cid]
				if ms.stopped {
					log.Info("Container stopped : ", ms.cid)
					cm.lock.Lock()
					delete(cm.monitorDb, cid)
					cm.lock.Unlock()
					continue
				}
				s, ok := <-ms.stats
				if ok {
					log.Infof("Container: %s Stats: %v", ms.cid, s)
				}
			}
		}
	}()
}

func (cm *containerMonitor) startMonitor(CID string) error {

	var client *client.Client
	var err error

	if client, err = getDockerClient(); err != nil {
		log.Error("Failed to get docker client : ", err)
		return err
	}

	ms := &monitor{}
	ms.stats = make(chan *types.StatsJSON)
	ms.done = make(chan bool)
	ms.cid = CID

	defer cm.lock.Unlock()
	cm.lock.Lock()
	cm.monitorDb[CID] = ms

	go ms.monitor(client)

	return nil
}

func Run() {
	log.Info("...........Starting Container Monitoring Service: %s ", time.Now().String())

	var client *client.Client
	var err error

	if client, err = getDockerClient(); err != nil {
		for err != nil {
			client, err = getDockerClient()
			time.Sleep(time.Minute * 1)
		}
	}

	cm := New()
	cm.monitorRunningContainers()
	cm.collector()

	f := filters.NewArgs()
	f.Add("type", "container")
	options := types.EventsOptions{
		Filters: f,
	}

	listener, errorChan := client.Events(context.Background(), options)
	for {
		select {
		case merr := <-errorChan:
			log.Error("Error on event channel", merr.Error())
		case event := <-listener:
			switch event.Status {
			case "start":
				cm.startMonitor(event.ID)
			case "die":
				log.Infof("..........Container dead CID : %s", event.ID)
				if ms, ok := cm.monitorDb[event.ID]; ok {
					ms.done <- true
				}
			}
		}
	}
}

func main() {
	Run()
}
