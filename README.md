`gossip` is a lightweight reverse proxy that routes Alertmanager webhook receiver notifications within Yandex Messenger. It has no configuration file, and assumes that:
- [ ] the `token` variable that contains a header type followed by a bot token has been set within the runtime environment
- [ ] `common.tmpl` is in the `proxy/static` directory during the build
- [ ] `chat_id` is in the URL path of the incoming request

### Usage

Alertmanager webhook receiver example:
```go
- name: devops
  webhook_configs:
  - url: "http://localhost:9055/{{ chat_id['devops'] }}"
```
Running in a container:
```go
docker buildx build -t gossip:latest .
docker run --rm --name gossip --network host -e "token=$token" gossip:latest
```
### Features

- [x] embedded static files
- [x] minimum third-party dependencies
- [x] `resolved` alerts answer `firing`
