#!/usr/bin/env bats

function setup {

  # run plugin
  run docker rm routed-test
  docker run -d --name routed-test --privileged --net=host -v /run/docker/plugins:/run/docker/plugins routed-plugin --gateway 10.100.0.1 --mtu 9000

  # create network
  run docker network rm test
  docker network create --internal --driver=net-routed --ipam-driver=ipam-routed --subnet 10.1.0.0/16 test
}

function teardown {

  # remove network test
  docker network rm test

  # kill plugin process
  docker stop routed-test
  docker rm routed-test
}

@test "Check network setup" {

  run docker run --rm -ti --net=test --ip=10.1.2.3 alpine ip addr
  [[ "$status" -eq 0 ]]
  [[ "{$output}" == *"10.1.2.3"* ]]

  run docker run --rm -ti --net=test --ip=10.1.2.3 alpine ip route
  [[ "$status" -eq 0 ]]
  [[ "${output}" == *"default via 10.100.0.1 dev eth0"* ]]
}

@test "Check container communication" {

  run docker rm routed-nc-test

  docker run -d --name routed-nc-test --net=test --ip=10.1.1.1 alpine nc -l -p 9999 -s 10.1.1.1
  run docker run --rm --net=test --ip=10.1.1.2 alpine sh -c 'echo foo|nc 10.1.1.1 9999'
  [[ "$status" -eq 0 ]]

  run docker logs $(docker ps -qa --filter name=routed-nc-test)
  [[ "${output}" == *"foo"* ]]

  docker stop routed-nc-test
  docker rm routed-nc-test
}

# ARP table should only contain entries for the default gw mac address 
@test "Check arp table" {

  run docker run --rm -ti --net=test --ip=10.1.2.3 alpine sh -c "ping -c 3 8.8.8.8 > /dev/null; ping -c 3 8.8.4.4 > /dev/null; sleep 2; arp -a"
  [ "$status" -eq 0 ]
  [[ "${output}" == *"10.100.0.1"* ]]
  [[ "${output}" != *"8.8.8.8"* ]]
  [[ "${output}" != *"8.8.4.4"* ]]
}
