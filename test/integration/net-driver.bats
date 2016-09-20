#!/usr/bin/env bats

function setup {

  # run plugin
  nohup docker run --privileged --net=host --rm -v /run/docker/plugins:/run/docker/plugins routed-plugin --gateway 10.100.0.1 --mtu 9000 --debug > $BATS_TMPDIR/routed_plugin_test.out 2>&1 &
  echo $! > $BATS_TMPDIR/routed_plugin_test.pid

  # create network
  run docker network rm test
  docker network create --internal --driver=net-routed --ipam-driver=ipam-routed --subnet 10.1.0.0/16 test
}

function teardown {

  # remove network test
  docker network rm test

  # kill plugin process
  kill $(<"$BATS_TMPDIR/routed_plugin_test.pid")
}

@test "Check network setup" {

  run docker run -ti --net=test --ip=10.1.2.3 alpine ip addr
  [ "$status" -eq 0 ]
  [[ "${output}" == *"10.1.2.3"* ]]

  run docker run -ti --net=test --ip=10.1.2.3 alpine ip route
  [ "$status" -eq 0 ]
  [[ "${output}" == *"default via 10.100.0.1 dev eth0"* ]]
}

@test "Check container communication" {

  nohup docker run --net=test --ip=10.1.1.1 alpine nc -l -p 9999 -s 10.1.1.1 > $BATS_TMPDIR/routed_plugin_nc_test.out 2>&1 &
  echo $! > $BATS_TMPDIR/routed_plugin_nc_test.pid
  run docker run --net=test --ip=10.1.1.2 alpine sh -c 'echo foo|nc 10.1.1.1 9999'
  [ "$status" -eq 0 ]

  run cat $BATS_TMPDIR/routed_plugin_nc_test.out
  [[ "${output}" == *"foo"* ]]

  run kill $(<"$BATS_TMPDIR/routed_plugin_nc_test.pid")
}

@test "Check arp table size" {

  run docker run -ti --net=test --ip=10.1.2.3 alpine sh -c "ping -c 3 8.8.8.8 > /dev/null; ping -c 3 8.8.4.4 > /dev/null; sleep 2; arp -a | wc -l"
  [ "$status" -eq 0 ]
  [[ "${output}" == "2"* ]]
}
