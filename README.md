`gossip` is a reverse proxy that routes Alertmanager webhook receiver notifications within Yandex Messenger.

### Build
Clone the repository. Follow the [Binary](https://github.com/rchnmy/gossip/tree/main?tab=readme-ov-file#binary) or [Docker](https://github.com/rchnmy/gossip/tree/main?tab=readme-ov-file#docker) section, depending on your preferences.
```
git clone https://github.com/rchnmy/gossip && cd gossip
```
#### Binary
Run Makefile and add the binary file to your `$PATH`.
```
make
sudo cp gossip /usr/local/bin/
```
Create `gossip` user and an application folder.
```
sudo useradd -M -s /bin/false gossip
sudo mkdir /etc/gossip
sudo cp -r gossip.yml templates /etc/gossip
sudo chown -R gossip:gossip /etc/gossip
```  
Configure a `systemd` unit.
```
sudo cp gossip.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now gossip
```

#### Docker
> [!NOTE]
> `distroless/static-debian11` as a runtime image doesn't provide a container with the shell and GNU core utils.

Build the Docker image. 
```
docker buildx build -t gossip:1.0 .
```
Run a `gossip` container mounting configuration file and templates in it.
```
docker run --rm --network host --volume "/etc/gossip:/etc/gossip:ro" --name gossip gossip:1.0
```

### Usage
> [!NOTE]
> Yandex Messenger doesn't support HTML tags.

By default, `gossip` looks for a configuration at `/etc/gossip/gossip.yml`. To set another path, use the `-c` flag.
```
gossip -c ~/gossip.yml
```
Format strings within templates using Markdown. Call the `Upper` and `Title` functions to convert them between cases.
```
🔥 **{{ .Labels.severity | Title }}**
```
