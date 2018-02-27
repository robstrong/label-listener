package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/fsouza/go-dockerclient"
)

const (
	ServiceHost = "land.strong.service.host"
	ServiceName = "land.strong.service.name"
)

var (
	addr     = flag.String("docker-addr", "unix:///var/run/docker.sock", "")
	httpPort = flag.String("http-port", ":80", "")

	services   = map[string]*Service{}
	servicesMu = sync.Mutex{}
	serviceTTL = time.Minute

	nextCacheClear         = time.Now().Add(serviceTTL)
	checkContainerInterval = time.Second * 10
)

func main() {
	flag.Parse()

	handleShutdownSignals()

	//start docker endpoint listener
	log.Println("starting docker listener")
	go serviceCache(startDockerListener(*addr), hasServiceLabels)

	//start http server
	log.Printf("starting http server on %s\n", *httpPort)
	err := http.ListenAndServe(*httpPort, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Println("http request received")
		servicesMu.Lock()
		serviceList := make([]*Service, len(services))
		i := 0
		for _, s := range services {
			serviceList[i] = s
			i++
		}
		servicesMu.Unlock()

		sort.Sort(ByName(serviceList))

		b, err := json.Marshal(serviceList)
		if err != nil {
			log.Printf("error marshalling json: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}))
	if err != nil {
		log.Fatalf("error: %v", err)
	}
}

type ByName []*Service

func (a ByName) Len() int           { return len(a) }
func (a ByName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByName) Less(i, j int) bool { return a[i].Name < a[j].Name }

func handleShutdownSignals() {
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGTERM)
	signal.Notify(signalCh, os.Interrupt)
	go func() {
		for range signalCh {
			os.Exit(0)
		}
	}()
}

type containerFilter func(c docker.APIContainers) bool

func hasServiceLabels(c docker.APIContainers) bool {
	if c.Labels[ServiceHost] != "" && c.Labels[ServiceName] != "" {
		return true
	}
	return false
}

func startDockerListener(addr string) chan docker.APIContainers {
	client, err := docker.NewClient(addr)
	if err != nil {
		log.Fatalf("could not connect to docker: %v", err)
	}
	containerCh := make(chan docker.APIContainers, 1)
	go func(client *docker.Client) {
		for range time.Tick(checkContainerInterval) {
			containers, err := client.ListContainers(docker.ListContainersOptions{All: false})
			if err != nil {
				log.Fatalf("could not list containers: %v", err)
			}
			for _, c := range containers {
				containerCh <- c
			}
		}
	}(client)

	return containerCh
}

func serviceCache(c chan docker.APIContainers, filter containerFilter) {
	for s := range c {
		if !filter(s) {
			continue
		}

		servicesMu.Lock()
		if nextCacheClear.Before(time.Now()) {
			clearCache()
		}

		if _, ok := services[s.Labels[ServiceHost]]; !ok {
			log.Printf("found new service: %s (%s)\n", s.Labels[ServiceName], s.Labels[ServiceHost])
		}
		services[s.Labels[ServiceHost]] = &Service{
			lastUpdated: time.Now().Add(serviceTTL),
			Addr:        s.Labels[ServiceHost],
			Name:        s.Labels[ServiceName],
		}
		servicesMu.Unlock()
	}
}

func clearCache() {
	for k, s := range services {
		if s.lastUpdated.Add(serviceTTL).Before(time.Now()) {
			delete(services, k)
		}
	}
	nextCacheClear = time.Now().Add(serviceTTL)
}

type Service struct {
	lastUpdated time.Time

	Name string
	Addr string
}
