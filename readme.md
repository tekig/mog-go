# Run Docker
1. Create a `config.json` file in `/opt/mog/config`
2. Fill in all fields using the example file in the repository [cfg/example.json](https://github.com/tekig/mog-go/blob/master/cfg/example.json)
3. Run docker image
```bash
docker run -d \
  --name mog \
  --restart always \
  -v /opt/mog/config:/app/cfg \
  -v /opt/mog/data:/app/data \
  ghcr.io/tekig/mog-go:master
```