echo off
echo start consul

consul agent -dev -config-dir ../consul_conf -bind=127.0.0.1 &


