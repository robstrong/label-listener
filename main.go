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
	httpPort = flag.String("http-port", ":80", "")

	services   = map[string]*Service{}
	servicesMu = sync.Mutex{}
	serviceTTL = time.Minute

	nextCacheClear = time.Now().Add(serviceTTL)
)

func main() {
	flag.Parse()

	handleShutdownSignals()

	//start docker endpoint listener
	log.Println("starting docker listener")
	go serviceCache(startDockerListener(*addr, containerFilterAll))

	//start http server
	log.Printf("starting http server on %d\n", *httpPort)
	err := http.ListenAndServe(*httpPort, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Println("http request received")
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

func handleShutdownSignals() {
	signalCh := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM)
	signal.Notify(c, os.Interrupt)
	go func() {
		for s := range signalCh {
			os.Exit(0)
		}
	}()
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
			log.Printf("found %d docker containers", len(containers))
			for _, c := range containers {
				if filter != nil && !filter(c) {
						continue
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
