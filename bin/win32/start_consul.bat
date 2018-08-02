echo off
echo start consul

start /b consul agent -dev -config-dir ../consul_conf
