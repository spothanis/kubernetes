# Kubernetes
Kubernetes is an open source implementation of container cluster management.

[Kubernetes Design Document](https://github.com/GoogleCloudPlatform/kubernetes/blob/master/DESIGN.md) - [Kubernetes @ Google I/O 2014](http://youtu.be/tsk0pWf4ipw)

[![GoDoc](https://godoc.org/github.com/GoogleCloudPlatform/kubernetes?status.png)](https://godoc.org/github.com/GoogleCloudPlatform/kubernetes)
[![Travis](https://travis-ci.org/GoogleCloudPlatform/kubernetes.svg?branch=master)](https://travis-ci.org/GoogleCloudPlatform/kubernetes)


## Kubernetes can run anywhere!
However, initial development was done on GCE and so our instructions and scripts are built around that.  If you make it work on other infrastructure please let us know and contribute instructions/code.

## Kubernetes is in pre-production beta!
While the concepts and architecture in Kubernetes represent years of experience designing and building large scale cluster manager at Google, the Kubernetes project is still under heavy development.  Expect bugs, design and API changes as we bring it to a stable, production product over the coming year.

### Contents
* Getting Started Guides
  * [Google Compute Engine](docs/getting-started-guides/gce.md)
  * [Vagrant](docs/getting-started-guides/vagrant.md)
  * [Locally](docs/getting-started-guides/locally.md)
  * [CoreOS](docs/getting-started-guides/coreos.md)
  * [Fedora](docs/getting-started-guides/fedora.md)
* [kubecfg command line tool](https://github.com/GoogleCloudPlatform/kubernetes/blob/master/docs/cli.md)
* [Kubernetes API Documentation](http://cdn.rawgit.com/GoogleCloudPlatform/kubernetes/ce4fcc4ad89ed7b481a46a0ad4c99dee4c6f24ba/api/kubernetes.html)
* [Discussion and Community Support](#community-discussion-and-support)
* [Hacking on Kubernetes](#development)

## Where to go next?
[Detailed example application](https://github.com/GoogleCloudPlatform/kubernetes/blob/master/examples/guestbook/README.md)

[Example of dynamic updates](https://github.com/GoogleCloudPlatform/kubernetes/blob/master/examples/update-demo/README.md)

Or fork and start hacking!

## Community, discussion and support

If you have questions or want to start contributing please reach out.  We don't bite!

The Kubernetes team is hanging out on IRC on the [#google-containers room on freenode.net](http://webchat.freenode.net/?channels=google-containers).  We also have the [google-containers Google Groups mailing list](https://groups.google.com/forum/#!forum/google-containers).

If you are a company and are looking for a more formal engagement with Google around Kubernetes and containers at Google as a whole, please fill out [this form](https://docs.google.com/a/google.com/forms/d/1_RfwC8LZU4CKe4vKq32x5xpEJI5QZ-j0ShGmZVv9cm4/viewform). and we'll be in touch.

## Development

### Go development environment

Kubernetes is written in [Go](http://golang.org) programming language. If you haven't set up Go development environment, please follow [this instruction](http://golang.org/doc/code.html) to install go tool and set up GOPATH.

### Put kubernetes into GOPATH

We highly recommend to put kubernetes' code into your GOPATH. For example, the following commands will download kubernetes' code under the current user's GOPATH (Assuming there's only one directory in GOPATH.):

```
$ echo $GOPATH
/home/user/goproj
$ mkdir -p $GOPATH/src/github.com/GoogleCloudPlatform/
$ cd $GOPATH/src/github.com/GoogleCloudPlatform/
$ git clone git@github.com:GoogleCloudPlatform/kubernetes.git
```

The commands above will not work if there are more than one directory in ``$GOPATH``.

### godep and dependency management

Kubernetes uses [godep](https://github.com/tools/godep) to manage dependencies. Please make sure that ``godep`` is installed and in your ``$PATH``. If you have already set up Go development environment correctly, the following command will install ``godep`` into your ``$GOBIN`` directory, which is ``$GOPATH/bin`` by default if ``$GOBIN`` is not set:

```
go get github.com/tools/godep
```

Here is a quick summary of `godep`.  `godep` helps manage third party dependencies by copying known versions into Godep/_workspace.  You can use `godep` in three ways:

1. Use `godep` to call your `go` commands.  For example: `godep go test ./...`
2. Use `godep` to modify your `$GOPATH` so that other tools know where to find the dependencies.  Specifically: `export GOPATH=$GOPATH:$(godep path)`
3. Use `godep` to copy the saved versions of packages into your `$GOPATH`.  This is done with `godep restore`.

We recommend using options #1 or #2.

### Hooks

Before committing any changes, please link/copy these hooks into your .git
directory. This will keep you from accidentally committing non-gofmt'd go code.

**NOTE:** The `../..` part seems odd but is correct, since the newly created
links will be 2 levels down the tree.

```
cd kubernetes
ln -s ../../hooks/prepare-commit-msg .git/hooks/prepare-commit-msg
ln -s ../../hooks/commit-msg .git/hooks/commit-msg
```

### Unit tests

```
cd kubernetes
hack/test-go.sh
```

Alternatively, you could also run:

```
cd kubernetes
godep go test ./...
```

If you only want to run unit tests in one package, you could run ``godep go test`` under the package directory. For example, the following commands will run all unit tests in package kubelet:

```
$ cd kubernetes # step into kubernetes' directory.
$ cd pkg/kubelet
$ godep go test
# some output from unit tests
PASS
ok      github.com/GoogleCloudPlatform/kubernetes/pkg/kubelet   0.317s
```

### Coverage
```
cd kubernetes
godep go tool cover -html=target/c.out
```

### Integration tests

You need an etcd somewhere in your path. To get from head:

```
go get github.com/coreos/etcd
go install github.com/coreos/etcd
sudo ln -s "$GOPATH/bin/etcd" /usr/bin/etcd
# Or just use the packaged one:
sudo ln -s "$REPO_ROOT/target/bin/etcd" /usr/bin/etcd
```

```
cd kubernetes
hack/integration-test.sh
```

### End-to-End tests

With a GCE account set up for running `cluster/kube-up.sh` (see Setup above):

```
cd kubernetes
hack/e2e-test.sh
```

### Add/Update dependencies

Kubernetes uses [godep](https://github.com/tools/godep) to manage dependencies. To add or update a package, please follow the instructions on [godep's document](https://github.com/tools/godep).

To add a new package ``foo/bar``:

- Download foo/bar into the first directory in GOPATH: ``go get foo/bar``.
- Change code in kubernetes to use ``foo/bar``.
- Run ``godep save ./...`` under kubernetes' root directory.

To update a package ``foo/bar``:

- Update the package with ``go get -u foo/bar``.
- Change code in kubernetes accordingly if necessary.
- Run ``godep update foo/bar``.

### Keeping your development fork in sync

One time after cloning your forked repo:

```
git remote add upstream https://github.com/GoogleCloudPlatform/kubernetes.git
```

Then each time you want to sync to upstream:

```
git fetch upstream
git rebase upstream/master
```

### Regenerating the API documentation

```
cd kubernetes/api
sudo docker build -t kubernetes/raml2html .
sudo docker run --name="docgen" kubernetes/raml2html
sudo docker cp docgen:/data/kubernetes.html .
```

View the API documentation using htmlpreview (works on your fork, too):
```
http://htmlpreview.github.io/?https://github.com/GoogleCloudPlatform/kubernetes/blob/master/api/kubernetes.html
```
