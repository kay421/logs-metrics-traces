FROM envoyproxy/envoy-contrib:v1.24.1

COPY ./config ./config
CMD /usr/local/bin/envoy -c ./config/envoy.yaml
