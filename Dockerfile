FROM alpine:latest

MAINTAINER Chirag Tayal <chiragtayal@gmail.com>

COPY monitor/monitor /bin
COPY monitorInit.sh /bin/
ENTRYPOINT ["/bin/monitorInit.sh"]

