# Gumroad Mediaconverter (GRMC)

A video conversion service that transforms video files into HLS streaming formats using FFmpeg.

## Endpoints

### POST /convert

Requires authentication. Enqueues an internal job to convert a video.
If too many jobs are already processing, the request will be rejected with a 429 status code.

Pass an optional `video_url` (for example, a signed CloudFront URL) to download the source video over HTTPS instead of from `s3_video_uri`. Either one is required.

```shell
curl --basic -u $YOUR_API_KEY: -X POST $GRMC_SERVER/convert -H "Content-Type: application/json" -d '{ "id": "123456", "s3_video_uri": "s3://[......]", "s3_hls_dir_uri": "s3://[......]/hls/", "presets": ["hls_480p", "hls_720p"], "callback_url": "https://example.com/callback" }'

# HTTP 200 OK
# {"job_id":"f2fea87585"}
# or
# HTTP 429 Too Many Requests
```

### GET /status

Requires authentication. Returns list of jobs currently being processed.

```shell
curl --basic -u $YOUR_API_KEY: -X GET $GRMC_SERVER/status

# HTTP 200 OK
# {"running_jobs":[{"JobID":"f2fea87585","Request":{"s3_video_uri":"s3://[......]","s3_hls_dir_uri":"s3://[......]/hls/","presets":["hls_480p","hls_720p"],"id":"123456","callback_url":"https://example.com/callback"},"Status":"processing","ErrorMsg":"","StartTime":"2024-10-11T08:44:45.09950379Z"}]}
```

### GET /up

Returns 200 OK if the server is running and environment variables are set.

## Development

You need to set a `.env` at the root of the project.

```shell
# Via docker
docker run -p 7454:7454 --env-file .env $(docker build -f Dockerfile . -q)

# Outside of docker
make && dotenv -f .env ./mediaconverter

# Tests
make test
```

## Deployment to production

- Assuming you're using kamal to deploy
- Assuming you have a `.env.production` at the root of the project

```shell
# First time: dotenv -f ./.env.production kamal setup
dotenv -f ./.env.production kamal deploy
```

## License

Gumroad Mediaconverter is licensed under the [MIT License](LICENSE.md).
