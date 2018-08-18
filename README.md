# Docker-Container-Monitoring

Collect Docker Stats for all the container running on docker host. 
It listen to docker container events and can start/stop monitoring for any container which is being started, stopped or killed.

Requirement:
1. Docker
2. Go

How to use:
1. clone the repository on the docker host where docker container are running
2. go run main.go 

It collects Docker Stats for each container every collectorTick = 10 seconds

Future Work:
1. Extend code to collect docker stats for all the containers running in cluster 
2. Use GUI to display stats. [Prometheus or Kibana]
