# Simple Example

Run the Inference Gateway with the following command:

```bash
cp .env.example .env
docker run --rm -it --env-file .env -p 8080:8080 ghcr.io/inference-gateway/inference-gateway:latest
```
