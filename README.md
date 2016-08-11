# docker-routed-plugin

## Development env installation using Vagrant

1. Create working dir for docker-routed-plugin

```
$ mkdir -p ~/repos/docker-devel/go/src/github.com/medallia
$ cd ~/repos/docker-devel/go/src/github.com/medallia
$ git clone http://github.com/medallia/docker-routed-plugin
$ cp docker-routed-plugin/Vagrantfile ~/repos/docker-devel
```

2. Install vagrant (for OSX)

```
$ brew cask install vagrant
$ vagrant plugin install vagrant-reload
$ vagrant plugin install vagrant-vbguest
```

3. Initialize your vagrant VM

```
$ cd ~/repos/docker-devel
$ vagrant init ubuntu/trusty64
$ vagrant ssh
```

4. Set up you GO environment in the VM (see https://www.digitalocean.com/community/tutorials/how-to-install-go-1-6-on-ubuntu-14-04)

```
$ sudo apt-get update
$ sudo curl -O https://storage.googleapis.com/golang/go1.6.linux-amd64.tar.gz
$ sudo tar -xvf go1.6.linux-amd64.tar.gz
$ sudo mv go /usr/local
$ echo "export GOPATH=/vagrant/go:/vagrant/go/src/github.com/medallia/docker-routed-plugin" >> ~/.profile
$ echo "export PATH=$PATH:/vagrant/go/bin:/usr/local/go/bin" >> ~/.profile
$ source ~/.profile
```

5. Install govendor in the VM

```
$ go get -u github.com/kardianos/govendor
```

6. Install delve (go debugging tool) in the VM

```
$ go get github.com/derekparker/delve/cmd/dlv
```

7. Install docker in the VM (see https://docs.docker.com/engine/installation/linux/ubuntulinux/)

```
$ sudo apt-get install apt-transport-https ca-certificates
$ sudo apt-key adv --keyserver hkp://p80.pool.sks-keyservers.net:80 --recv-keys 58118E89F3A912897C070ADBF762215
$ sudo vim /etc/apt/sources.list.d/docker.list # add deb https://apt.dockerproject.org/repo ubuntu-trusty main
$ sudo apt-get update
$ sudo apt-get purge lxc-docker
$ apt-cache policy docker-engine
$ sudo apt-get update
$ sudo apt-get install linux-image-extra-$(uname -r)
$ sudo apt-get install docker-engine
$ sudo service docker start
$ sudo usermod -aG docker $USER
```

8. Finish setting up your development env in the VM

```
$ cd
$ mkdir repos
$ cd repos
$ ln -s /vagrant/go/src/github.com/medallia/docker-routed-plugin docker-plugin
$ curl -fsSLO https://get.docker.com/builds/Linux/x86_64/docker-1.12.0.tgz && tar --strip-components=1 -xvzf docker-1.12.0.tgz docker/docker && mv docker docker.ok && rm docker-1.12.0.tgz
$ git clone http://github.com/docker/docker && cd docker && git checkout 8eab29edd820017901796eb60d4bea28d760f16 && cd -
$ cp docker-plugin/docker-build.sh .
$ sudo bash docker-build.sh
```

## Usage

1. Build plugin docker image and run the driver

```
$ vagrant ssh
$ cd ~/repos/docker-routed-plugin
$ make docker-run
```

2. Test network creation (in another terminal)

```
$ vagrant ssh
$ cd ~/repos/docker-routed-plugin
$ docker network create --internal --driver=routed --subnet 10.46.0.0/16  mine
$ docker run -ti --net=mine --ip 10.46.1.7 debian:jessie sh
```

## Debugging with Delve

For info on Delve see https://blog.gopheracademy.com/advent-2015/debugging-with-delve
and https://github.com/derekparker/delve/tree/master/Documentation/cli

1. Run the plugin in a terminal

```
$ vagrant ssh
$ cd ~/repos/docker-routed-plugin
$ docker run -ti --privileged --net=host --rm -v /run/docker/plugins:/run/docker/plugins test/routed-plugin -log-level debug
```

2. In another terminal, attach delve to the plugin process

For breakpoint syntax see https://github.com/derekparker/delve/issues/528

```
$ vagrant ssh
$ ps afx | grep docker
$ sudo su
# export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/games:/usr/local/games:/vagrant/go/bin:/usr/local/go/bin:/vagrant/go/bin:/usr/local/go/bin
# dlv attach <pid>
(dlv) break /routed.*CreateNetwork/
(dlv) break /routed.*DeleteNetwork/
(dlv) break /routed.*NewDriver/
```

3. From yet another terminal, create a routed network in a docker container

```
$ vagrant ssh
$ cd ~/repos/docker-routed-plugin
$ docker network create --internal --driver=routed --subnet 10.46.0.0/16  mine
```

4. Now you can debug in the delve terminal

```
(dlv) continue
```
