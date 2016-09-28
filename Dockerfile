FROM debian:jessie
RUN apt-get update && apt-get -y install iptables

COPY routed-plugin /
RUN chmod +x /routed-plugin

ENTRYPOINT ["./routed-plugin"]
