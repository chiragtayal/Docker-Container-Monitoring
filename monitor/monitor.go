package main

import (
	"context"
	"encoding/json"
	"os"
	"runtime/trace"
	"sync"
	"time"
	//"unsafe"

	log "github.com/sirupsen/logrus"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

const (
	statsTick = 10
)

type containerMonitor struct {
	monitorDb map[string]*monitor
	lock      sync.Mutex
}

type monitor struct {
	stats   chan types.StatsJSON
	done    chan bool
	cid     string
	stopped bool
}

func (ms *monitor) monitor(cli *client.Client) {

	stats, err := cli.ContainerStats(context.Background(), ms.cid, true)
	if err != nil {
		log.Errorf("Unable to gather stats for %s container, reason: %s", ms.cid, err.Error())
		ms.stopped = true
		return
	}

	dec := json.NewDecoder(stats.Body)

	go func() {
		log.Info("Start Monitoring for Container : ", ms.cid)
		for {
			select {
			case <-ms.done:
				log.Error("End Monitoring for Container : ", ms.cid)
				stats.Body.Close()
				ms.stopped = true
				return
			default:
				var v types.StatsJSON
				if err := dec.Decode(&v); err != nil {
					log.Error("Unable to decode stats : ", err.Error())
					ms.stopped = true
					return
				}
				//log.Infof("Size of paypload: %d", unsafe.Sizeof(v))
				ms.stats <- v
			}
		}
	}()
}

func (cm *containerMonitor) startMonitor(client *client.Client, CID string) {

	ms := monitor{
		stats: make(chan types.StatsJSON),
		done:  make(chan bool),
		cid:   CID,
	}
	defer cm.lock.Unlock()
	cm.lock.Lock()
	cm.monitorDb[CID] = &ms

	ms.monitor(client)
}

func (cm *containerMonitor) monitorRunningContainers(client *client.Client) {

	containers, err := client.ContainerList(context.Background(), types.ContainerListOptions{})
	if err != nil {
		return
	}

	for _, container := range containers {
		cm.startMonitor(client, container.ID)
	}
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

func New() *containerMonitor {

	var cminstance containerMonitor

	log.Info("Creating Container Monitor Instance")

	cminstance = containerMonitor{
		monitorDb: make(map[string]*monitor),
	}

	return &cminstance
}

func Run() {
	log.Info("...........Starting Container Monitoring Service: %s ", time.Now().String())

	var dclient *client.Client
	var err error

	dclient, err = client.NewClient("unix:///var/run/docker.sock", "v1.18", nil,
		map[string]string{"User-Agent": "engine-api-cli-1.0"})
	if err != nil {
		log.Errorf("Unable to create docker client: %v", err)
		return
	}

	cm := New()
	cm.monitorRunningContainers(dclient)
	cm.collector()

	f := filters.NewArgs()
	f.Add("type", "container")
	options := types.EventsOptions{
		Filters: f,
	}

	listener, errorChan := dclient.Events(context.Background(), options)
	for {
		select {
		case merr := <-errorChan:
			log.Error("Error on event channel", merr.Error())
		case event := <-listener:
			switch event.Status {
			case "start":
				cm.startMonitor(dclient, event.ID)
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
	trace.Start(os.Stdout)
	defer trace.Stop()

	Run()
}
