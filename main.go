package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/fsouza/go-dockerclient"
)

const (
	ServiceHost = "land.strong.service.host"
	ServiceName = "land.strong.service.name"
)

var (
	addr = flag.String("docker-addr", "unix:///var/run/docker.sock", "")

	services   = map[string]*Service{}
	servicesMu = sync.Mutex{}
	serviceTTL = time.Minute

	nextCacheClear = time.Now().Add(serviceTTL)
)

func main() {
	flag.Parse()

	//start docker endpoint listener
	go serviceCache(startDockerListener(*addr, containerFilterAll))

	//start http server
	err := http.ListenAndServe(*addr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := json.Marshal(r)
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

type containerFilter func(c docker.APIContainers) bool

func containerFilterAll(c docker.APIContainers) bool {
	return true
}

func startDockerListener(addr string, filter containerFilter) chan docker.APIContainers {
	client, err := docker.NewClient(addr)
	if err != nil {
		log.Fatalf("could not connect to docker: %v", err)
	}
	containerCh := make(chan docker.APIContainers, 1)
	go func(client *docker.Client) {
		for range time.Tick(time.Minute) {
			containers, err := client.ListContainers(docker.ListContainersOptions{All: false})
			if err != nil {
				log.Fatalf("could not list containers: %v", err)
			}
			for _, c := range containers {
				if filter != nil {
					if !filter(c) {
						continue
					}
				}
				containerCh <- c
			}
		}
	}(client)

	return containerCh
}

func serviceCache(c chan docker.APIContainers) {
	for s := range c {
		servicesMu.Lock()
		if nextCacheClear.Before(time.Now()) {
			clearCache()
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
