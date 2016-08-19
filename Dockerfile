FROM debian:jessie

COPY routed-plugin /
RUN chmod +x /routed-plugin

ENTRYPOINT ["./routed-plugin"]
