# docker-routed-plugin

## Configuring, testing and debugging 

1. Install vagrant

```
$ brew cask install vagrant
$ vagrant plugin install vagrant-reload
$ vagrant plugin install vagrant-vbguest
```

2. Create working dir

```
$ mkdir -p ~/repos/docker-devel/go/src/github.com/medallia
$ cd ~/repos/docker-devel/go/src/github.com/medallia
$ git clone http://github.com/medallia/docker-routed-plugin
```

3. Init your vagrant VM 

```
$ cd ~/repos/docker-devel
$ vagrant init ubuntu/trusty64
```

4. Set up you GO environment 

```
$ vagrant ssh
$ export GOPATH=/vagrant/go:/vagrant/go/src/github.com/medallia/docker-routed-plugin
$ export PATH=$PATH:/vagrant/go/bin
```

5. Install delve (go debugging tool) if not installed

```
$ go get github.com/derekparker/delve/cmd/dlv
```

6. Run the tests

```
$ cd /vagrant/go/src/github.com/medallia/docker-routed-plugin
$ TODO
```
