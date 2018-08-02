//go:generate protoc -I ../proto --go_out=plugins=grpc:../proto/ ../proto/server.proto ../proto/client.proto ../proto/common.proto ../proto/db.proto
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/golang/protobuf/proto"
	"github.com/syndtr/goleveldb/leveldb"
	"roisoft.com/hdlj/common"
	"roisoft.com/hdlj/common/config"
	pb "roisoft.com/hdlj/proto"
)

var (
	db  *leveldb.DB
	err error
)

func main() {
	ex, err := os.Executable()
	if err != nil {
		panic(err)
	}
	exPath := filepath.Dir(ex)
	//fmt.Println(exPath)
	var bf bytes.Buffer

	bf.WriteString(exPath)
	c, _ := config.ReadDefault(bf.String() + "/dbconfig.cfg")
	nodeno, err := c.String("DEFAULT", "node_id")
	if err != nil {
		fmt.Println(err)
	} else {
		pos := strings.LastIndex(nodeno, ".")
		if pos == -1 {
			pos = 0
		} else {
			pos++
		}
		nodeno = nodeno[pos:]
	}

	var node *int64
	if err != nil {
		fmt.Print(err)
		fmt.Println("using default node value")
		node = flag.Int64("node", 0, "Current workin node(file)")
	} else {
		node_id, _ := strconv.Atoi(nodeno)
		node = flag.Int64("node", int64(node_id), "Current workin node(file)")
	}

	putPtr := flag.String("put", "", "PUT operation")
	vPtr := flag.String("value", "", "value used with put flag")
	getPtr := flag.String("get", "", "GET operation")
	delPtr := flag.String("del", "", "DEL operation")

	file := flag.String("file", "ACCOUNT", "DB FILE")
	all := flag.Bool("all", false, "show all the data")
	addNode := flag.Int64("addn", 0, "The Node ID you want to add")
	before := flag.Int64("before", 0, "Add the node right before(another node)")
	delNode := flag.Int64("deln", 0, "The Node ID you want to delete")

	flag.Parse()

	var buffer, bufferadd, bufferbefore, bufferdel bytes.Buffer
	buffer.WriteString(exPath)
	buffer.WriteString("/" + strconv.Itoa(int(*node)))
	buffer.WriteString("/" + *file)
	path, err := filepath.Abs(buffer.String())
	if err != nil {
		fmt.Println(err)
	}
	fmt.Printf("Current working path : %s\n", path)

	var pathadd, pathbefore, pathdel string
	if *addNode != 0 {
		bufferadd.WriteString(exPath)
		bufferadd.WriteString("/" + strconv.Itoa(int(*addNode)))
		//bufferadd.WriteString("/" + *file)
		pathadd, err = filepath.Abs(bufferadd.String())
		if err != nil {
			fmt.Println(err)
		}
		fmt.Printf("Path to be added : %s\n", pathadd)
	}

	if *before != 0 {
		bufferbefore.WriteString(exPath)
		bufferbefore.WriteString("/" + strconv.Itoa(int(*before)))
		//bufferbefore.WriteString("/" + *file)
		pathbefore, err = filepath.Abs(bufferbefore.String())
		if err != nil {
			fmt.Println(err)
		}
		fmt.Printf("Path add/del before : %s\n", pathbefore)
	}

	if *delNode != 0 {
		bufferdel.WriteString(exPath)
		bufferdel.WriteString("/" + strconv.Itoa(int(*delNode)))
		//bufferdel.WriteString("/" + *file)
		pathdel, err = filepath.Abs(bufferdel.String())
		if err != nil {
			fmt.Println(err)
		}
		fmt.Printf("Path to be deleted : %s\n", pathdel)
	}

	if *addNode != 0 && *before != 0 {
		if *addNode > *before {
			fmt.Println("addnode > beforenode")
			return
		}
		addNodef(pathadd, pathbefore, *addNode)
		return
	}
	if *delNode != 0 && *before != 0 {
		if *delNode > *before {
			fmt.Println("delNode > beforenode")
			return
		}
		delNodef(pathdel, pathbefore)
		return
	}

	db, err = leveldb.OpenFile(path, nil)
	if err != nil {
		fmt.Println(err)
	}

	if *all {
		iter := db.NewIterator(nil, nil)
		for iter.Next() {
			// Remember that the contents of the returned slice should not be modified, and
			// only valid until the next call to Next.
			key := iter.Key()
			value := iter.Value()
			fmt.Printf("(%s : %s) \n", key, value)
		}
		iter.Release()
		err = iter.Error()
		if err != nil {
			fmt.Print(err)
		}
	}

	if *putPtr != "" && *vPtr != "" {
		err := db.Put([]byte(*putPtr), []byte(*vPtr), nil)
		if err != nil {
			fmt.Println(err)
			return
		} else {
			fmt.Printf("(%s : %s) added\n", *putPtr, *vPtr)
		}
	}

	if *getPtr == *delPtr && *getPtr != "" {
		panic("Only support unary operation on the same key!")
	}

	if *getPtr != "" {
		data, err := db.Get([]byte(*getPtr), nil)
		if err != nil {
			//fmt.Printf("%v\n",err)
			fmt.Println(err)
			return
		}
		fmt.Printf("(%s : %s)\n", *getPtr, data)
	}

	if *delPtr != "" {
		err := db.Delete([]byte(*delPtr), nil)
		if err != nil {
			fmt.Println(err)
			return
		} else {
			fmt.Printf("key : %s is deleted\n", *delPtr)
		}
	}
}

func addNodef(add string, before string, nodeid int64) {
	dbAddAcc, err := leveldb.OpenFile(add+"/ACCOUNT", nil)
	if err != nil {
		fmt.Println(err)
	}
	dbAddGame, err := leveldb.OpenFile(add+"/GAME", nil)
	if err != nil {
		fmt.Println(err)
	}
	dbAddAccIndex, err := leveldb.OpenFile(add+"/ACCOUNT/INDEX", nil)
	if err != nil {
		fmt.Println(err)
	}
	dbBfAcc, err := leveldb.OpenFile(before+"/ACCOUNT", nil)
	if err != nil {
		fmt.Println(err)
	}
	dbBfGame, err := leveldb.OpenFile(before+"/GAME", nil)
	if err != nil {
		fmt.Println(err)
	}
	dbBfAccIndex, err := leveldb.OpenFile(before+"/ACCOUNT/INDEX", nil)
	if err != nil {
		fmt.Println(err)
	}

	iterAcc := dbBfAcc.NewIterator(nil, nil)
	for iterAcc.Next() {
		// Remember that the contents of the returned slice should not be modified, and
		// only valid until the next call to Next.
		key := iterAcc.Key()
		value := iterAcc.Value()
		var pid pb.KeyPlayerId
		proto.Unmarshal(key, &pid)
		node_value := common.GetNodeValueByPlayerId(pid.Key)
		fmt.Println(node_value)
		if int64(node_value) < nodeid {
			err = dbAddAcc.Put(key, value, nil)
			if err != nil {
				fmt.Print(err)
			}
			err = dbBfAcc.Delete(key, nil)
			if err != nil {
				fmt.Print(err)
			}
			fmt.Printf("(%s : %s) \n", key, value)
		}

	}
	iterAcc.Release()
	err = iterAcc.Error()
	if err != nil {
		fmt.Print(err)
	}

	iterGame := dbBfGame.NewIterator(nil, nil)
	for iterGame.Next() {
		// Remember that the contents of the returned slice should not be modified, and
		// only valid until the next call to Next.
		key := iterAcc.Key()
		value := iterAcc.Value()
		var pid pb.KeyPlayerId
		proto.Unmarshal(key, &pid)
		node_value := common.GetNodeValueByPlayerId(pid.Key)

		if int64(node_value) < nodeid {
			err = dbAddGame.Put(key, value, nil)
			if err != nil {
				fmt.Print(err)
			}
			err = dbBfGame.Delete(key, nil)
			if err != nil {
				fmt.Print(err)
			}
			fmt.Printf("(%s : %s) \n", key, value)
		}

	}
	iterGame.Release()
	err = iterGame.Error()
	if err != nil {
		fmt.Print(err)
	}

	iterAccIndex := dbBfAccIndex.NewIterator(nil, nil)
	for iterAccIndex.Next() {
		// Remember that the contents of the returned slice should not be modified, and
		// only valid until the next call to Next.
		key := iterAcc.Key()
		value := iterAcc.Value()
		var ps pb.KeyString
		proto.Unmarshal(key, &ps)
		node_value := common.GetNodeValueByString(ps.Key)

		if int64(node_value) < nodeid {
			err = dbAddAccIndex.Put(key, value, nil)
			if err != nil {
				fmt.Print(err)
			}
			err = dbBfAccIndex.Delete(key, nil)
			if err != nil {
				fmt.Print(err)
			}
			fmt.Printf("(%s : %s) \n", key, value)
		}

	}
	iterAccIndex.Release()
	err = iterAccIndex.Error()
	if err != nil {
		fmt.Print(err)
	}
}

func delNodef(del string, before string) {
	dbDelAcc, err := leveldb.OpenFile(del+"/ACCOUNT", nil)
	if err != nil {
		fmt.Println(err)
	}
	dbDelGame, err := leveldb.OpenFile(del+"/GAME", nil)
	if err != nil {
		fmt.Println(err)
	}
	dbBfAcc, err := leveldb.OpenFile(before+"/ACCOUNT", nil)
	if err != nil {
		fmt.Println(err)
	}
	dbBfGame, err := leveldb.OpenFile(before+"/GAME", nil)
	if err != nil {
		fmt.Println(err)
	}
	dbDelAccIndex, err := leveldb.OpenFile(del+"/ACCOUNT/INDEX", nil)
	if err != nil {
		fmt.Println(err)
	}
	dbBfAccIndex, err := leveldb.OpenFile(del+"/ACCOUNT/INDEX", nil)
	if err != nil {
		fmt.Println(err)
	}

	iterAcc := dbDelAcc.NewIterator(nil, nil)
	for iterAcc.Next() {
		// Remember that the contents of the returned slice should not be modified, and
		// only valid until the next call to Next.
		key := iterAcc.Key()
		value := iterAcc.Value()

		err = dbBfAcc.Put(key, value, nil)
		if err != nil {
			fmt.Print(err)
		}
		err = dbDelAcc.Delete(key, nil)
		if err != nil {
			fmt.Print(err)
		}
		fmt.Printf("(%s : %s) \n", key, value)
	}
	iterAcc.Release()
	err = iterAcc.Error()
	if err != nil {
		fmt.Print(err)
	}

	iterAccIndex := dbDelAcc.NewIterator(nil, nil)
	for iterAccIndex.Next() {
		// Remember that the contents of the returned slice should not be modified, and
		// only valid until the next call to Next.
		key := iterAccIndex.Key()
		value := iterAccIndex.Value()

		err = dbBfAccIndex.Put(key, value, nil)
		if err != nil {
			fmt.Print(err)
		}
		err = dbDelAccIndex.Delete(key, nil)
		if err != nil {
			fmt.Print(err)
		}
		fmt.Printf("(%s : %s) \n", key, value)
	}
	iterAccIndex.Release()
	err = iterAccIndex.Error()
	if err != nil {
		fmt.Print(err)
	}

	iterGame := dbDelGame.NewIterator(nil, nil)
	for iterGame.Next() {
		// Remember that the contents of the returned slice should not be modified, and
		// only valid until the next call to Next.
		key := iterGame.Key()
		value := iterGame.Value()

		err = dbBfGame.Put(key, value, nil)
		if err != nil {
			fmt.Print(err)
		}
		err = dbDelGame.Delete(key, nil)
		if err != nil {
			fmt.Print(err)
		}
		fmt.Printf("(%s : %s) \n", key, value)
	}
	iterGame.Release()
	err = iterGame.Error()
	if err != nil {
		fmt.Print(err)
	}
}
