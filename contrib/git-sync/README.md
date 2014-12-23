# git-sync

git-sync is a command that periodically sync a git repository to a local directory.

It can be used to source a container volume with the content of a git repo.

## Usage

```
# build the container
docker build -t git-sync .
# run the git-sync container
docker run -d -e GIT_SYNC_INTERVAL=1s -e GIT_SYNC_REPO=https://github.com/GoogleCloudPlatform/kubernetes -e GIT_SYNC_BRANCH=gh-pages -v /git-data:/git git-sync
# run a nginx container to serve sync'ed content
docker run -d -p 8080:80 -v /git-data/HEAD:/usr/share/nginx/html nginx 
```
