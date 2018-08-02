#mkdir
if [ ! -d ./bin ]; then
	mkdir ./bin
fi

#build proto
#cd ./game_server/
#go generate
#cd ..


#build hdlj
echo generate db_server
go build -o ./bin/db_server ./db_server
go build -o ./bin/db_tool ./db_server/db_tool
cp -i ./db_server/db_server.cfg ./bin/

echo generate master_server
go build -o ./bin/master_server ./master_server
cp -i ./master_server/master_server.cfg ./bin/
if [ ! -f ./bin/roisoft.playerid.ini ]; then
	cp ./master_server/roisoft.playerid.ini ./bin/
fi

echo generate web_server
go build -o ./bin/web_server ./web_server
cp -i ./web_server/web_server.cfg ./bin/

echo generate account_server
go build -o ./bin/account_server ./account_server
cp -i ./account_server/account_server.cfg ./bin/

echo generate game_server
go build -o ./bin/game_server ./game_server
cp -i ./game_server/game_server.cfg ./bin/
if [ ! -d ./bin/table ]; then
	mkdir ./bin/table
fi
cp ./game_server/table/*.* ./bin/table/

#chmod
chmod u+x ./bin/db_server
chmod u+x ./bin/master_server
chmod u+x ./bin/web_server
chmod u+x ./bin/account_server
chmod u+x ./bin/game_server
chmod u+x ./bin/linux/*.sh

echo generate hdlj complete. 
