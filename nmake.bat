echo off

for %%d in (.\bin;) do (if not exist %%d mkdir %%d )

cd game_server
go generate
cd ..

echo generate db_server
go build -o .\bin\db_server.exe .\db_server
go build -o .\bin\db_tool.exe .\db_server\db_tool
copy /-y .\db_server\db_server.cfg .\bin\

echo generate master_server
go build -o .\bin\master_server.exe .\master_server
copy /-y .\master_server\master_server.cfg .\bin\
if not exist .\bin\roisoft.playerid.ini (
	copy .\master_server\roisoft.playerid.ini .\bin\
)

echo generate web_server
go build -o .\bin\web_server.exe .\web_server
copy /-y .\web_server\web_server.cfg .\bin\

echo generate account_server
go build -o .\bin\account_server.exe .\account_server
copy /-y .\account_server\account_server.cfg .\bin\

echo generate game_server
go build -o .\bin\game_server.exe .\game_server
copy /-y .\game_server\game_server.cfg .\bin\
if not exist .\bin\table (
	mkdir .\bin\table
)
copy .\game_server\table\*.* .\bin\table\

echo generate hdlj complete.
