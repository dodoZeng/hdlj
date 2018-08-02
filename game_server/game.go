package main

import (
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/Luxurioust/excelize"
	"github.com/golang/glog"
	pb "roisoft.com/hdlj/proto"
)

// 装备
type tbl_equip_data struct {
	item_id  uint32
	is_show  bool
	rarity   uint8
	bm_price int32
	re_price int32
}

// 道具
type tbl_item_data struct {
	item_id    uint32
	gold_price int32
	soul_price int32
}

// 英雄
type tbl_hero_data struct {
	hero_id  uint32
	is_show  bool
	rarity   uint8
	bm_price int32
	re_price int32
}

// 稀有度ID列表
type rarity_id_set struct {
	init    []uint32
	normal  []uint32
	advance []uint32
	rare    []uint32
	legend  []uint32
}

var (
	mapEquip map[uint32]*tbl_equip_data
	mapItem  map[uint32]*tbl_item_data
	mapHero  map[uint32]*tbl_hero_data

	equip_id_set rarity_id_set
	hero_id_set  rarity_id_set

	slHero []int
)

func init() {
	mapEquip = make(map[uint32]*tbl_equip_data)
	mapItem = make(map[uint32]*tbl_item_data)
	mapHero = make(map[uint32]*tbl_hero_data)
}

func loadPushingTable(fp string) {
	xlsx, err := excelize.OpenFile(fp)
	if err == nil {
		rows := xlsx.GetRows("Sheet1")
		now := time.Now().UnixNano()
		for i, row := range rows {
			if i >= 1 {
				msg := pushing_message{}
				loc, _ := time.LoadLocation("Local")

				if row[3] == "" {
					continue
				}

				msg.msg_id = i
				msg.text = row[3]
				if v, err := strconv.ParseUint(row[0], 10, 32); err != nil {
					glog.Errorf("fail to parse. [col = %d, row = %d, err = %s, file = %s]\n", 0, i, err.Error(), fp)
					continue
				} else {
					msg.msg_type = uint(v)
				}
				if v, err := time.ParseInLocation("2006-01-02 15:04:05", row[1], loc); err != nil {
					glog.Errorf("fail to parse. [col = %d, row = %d, err = %s, file = %s]\n", 1, i, err.Error(), fp)
					continue
				} else {
					msg.ts_from = v.UnixNano()
				}
				if v, err := time.ParseInLocation("2006-01-02 15:04:05", row[2], loc); err != nil {
					glog.Errorf("fail to parse. [col = %d, row = %d, err = %s, file = %s]\n", 2, i, err.Error(), fp)
					continue
				} else {
					msg.ts_to = v.UnixNano()
				}

				if msg.ts_to > now {
					pushingManager.Store(i, &msg)
				}
			}
		}
		fmt.Printf("file(%s) load ok.\n", fp)
	}
}

func loadGameTable(path string) (bool, string) {

	// 装备表
	file := fmt.Sprintf("%s/%s", path, "EquipData.xlsx")
	xlsx, err := excelize.OpenFile(file)
	if err != nil {
		return false, err.Error()
	} else {
		rows := xlsx.GetRows("Sheet1")
		for i, row := range rows {
			if i >= 1 {
				var data tbl_equip_data

				if row[3] == "" {
					continue
				}

				if v, err := strconv.ParseUint(row[3], 10, 32); err != nil {
					return false, fmt.Sprintf("fail to parse EquipData.xlsx [err = %s]", err.Error())
				} else {
					data.item_id = uint32(v)
				}
				if v, err := strconv.ParseUint(row[22], 10, 32); err != nil {
					return false, fmt.Sprintf("fail to parse EquipData.xlsx [err = %s]", err.Error())
				} else {
					data.is_show = v > 0
				}
				if v, err := strconv.ParseUint(row[23], 10, 32); err != nil {
					return false, fmt.Sprintf("fail to parse EquipData.xlsx [err = %s]", err.Error())
				} else {
					data.rarity = byte(v)
				}
				if v, err := strconv.ParseInt(row[24], 10, 32); err != nil {
					return false, fmt.Sprintf("fail to parse EquipData.xlsx [err = %s]", err.Error())
				} else {
					data.bm_price = int32(v)
				}
				if v, err := strconv.ParseInt(row[25], 10, 32); err != nil {
					return false, fmt.Sprintf("fail to parse EquipData.xlsx [err = %s]", err.Error())
				} else {
					data.re_price = int32(v)
				}

				// 过滤初始装备
				if data.rarity == uint8(pb.Rarity_Initial) {
					continue
				}

				mapEquip[data.item_id] = &data

				if data.is_show {
					switch data.rarity {
					case byte(pb.Rarity_Initial):
						equip_id_set.init = append(equip_id_set.init, data.item_id)
					case byte(pb.Rarity_Normal):
						equip_id_set.normal = append(equip_id_set.normal, data.item_id)
					case byte(pb.Rarity_Advance):
						equip_id_set.advance = append(equip_id_set.advance, data.item_id)
					case byte(pb.Rarity_Rare):
						equip_id_set.rare = append(equip_id_set.rare, data.item_id)
					case byte(pb.Rarity_Legend):
						equip_id_set.legend = append(equip_id_set.legend, data.item_id)
					}
				}
			}
		}
		//fmt.Println("equip_id_set :", len(equip_id_set.normal), len(equip_id_set.advance), len(equip_id_set.rare), len(equip_id_set.legend))
	}

	// 道具表
	file = fmt.Sprintf("%s/%s", path, "ItemData.xlsx")
	xlsx, err = excelize.OpenFile(file)
	if err != nil {
		return false, err.Error()
	} else {
		rows := xlsx.GetRows("Sheet1")
		for i, row := range rows {
			if i >= 1 {
				var data tbl_item_data

				if row[2] == "" {
					continue
				}

				if v, err := strconv.ParseUint(row[2], 10, 32); err != nil {
					return false, fmt.Sprintf("fail to parse ItemData.xlsx [err = %s]", err.Error())
				} else {
					data.item_id = uint32(v)
				}
				if v, err := strconv.ParseInt(row[3], 10, 32); err != nil {
					return false, fmt.Sprintf("fail to parse ItemData.xlsx [err = %s]", err.Error())
				} else {
					data.gold_price = int32(v)
				}
				if v, err := strconv.ParseInt(row[4], 10, 32); err != nil {
					return false, fmt.Sprintf("fail to parse ItemData.xlsx [err = %s]", err.Error())
				} else {
					data.soul_price = int32(v)
				}

				mapItem[data.item_id] = &data
			}
		}
		//fmt.Println("mapItem:", mapItem)
	}

	// 英雄表
	file = fmt.Sprintf("%s/%s", path, "HeroData.xlsx")
	xlsx, err = excelize.OpenFile(file)
	if err != nil {
		return false, err.Error()
	} else {
		rows := xlsx.GetRows("Sheet1")
		for i, row := range rows {
			if i >= 1 {
				var data tbl_hero_data

				if row[3] == "" {
					continue
				}

				if v, err := strconv.ParseUint(row[3], 10, 32); err != nil {
					return false, fmt.Sprintf("fail to parse HeroData.xlsx [err = %s]", err.Error())
				} else {
					data.hero_id = uint32(v)
				}
				if v, err := strconv.ParseUint(row[18], 10, 32); err != nil {
					return false, fmt.Sprintf("fail to parse EquipData.xlsx [err = %s]", err.Error())
				} else {
					data.is_show = v > 0
				}
				if v, err := strconv.ParseUint(row[19], 10, 32); err != nil {
					return false, fmt.Sprintf("fail to parse EquipData.xlsx [err = %s]", err.Error())
				} else {
					data.rarity = byte(v)
				}
				if v, err := strconv.ParseInt(row[20], 10, 32); err != nil {
					return false, fmt.Sprintf("fail to parse EquipData.xlsx [err = %s]", err.Error())
				} else {
					data.bm_price = int32(v)
				}
				if v, err := strconv.ParseInt(row[21], 10, 32); err != nil {
					return false, fmt.Sprintf("fail to parse EquipData.xlsx [err = %s]", err.Error())
				} else {
					data.re_price = int32(v)
				}

				// 过滤初始英雄
				if data.rarity == uint8(pb.Rarity_Initial) {
					continue
				}

				mapHero[data.hero_id] = &data

				if data.is_show {
					switch data.rarity {
					case byte(pb.Rarity_Initial):
						hero_id_set.init = append(hero_id_set.init, data.hero_id)
					case byte(pb.Rarity_Normal):
						hero_id_set.normal = append(hero_id_set.normal, data.hero_id)
					case byte(pb.Rarity_Advance):
						hero_id_set.advance = append(hero_id_set.advance, data.hero_id)
					case byte(pb.Rarity_Rare):
						hero_id_set.rare = append(hero_id_set.rare, data.hero_id)
					case byte(pb.Rarity_Legend):
						hero_id_set.legend = append(hero_id_set.legend, data.hero_id)
					}
				}
			}
		}
	}

	return true, ""
}

func randRoll(options []uint8) int {
	n := rand.Intn(99)

	sum := 0
	for i, v := range options {
		sum = sum + int(v)
		if n < sum {
			return i
		}
	}

	return -1
}

func openTreasureBoxGray() (uint32, []uint32) {
	award_hero_id := uint32(0)
	award_item_ids := []uint32{}
	index := 0

	// 产生角色卡
	index = randRoll([]uint8{60, 40})
	if index == 1 {
		if total := len(hero_id_set.normal) - 1; total >= 0 {
			award_hero_id = hero_id_set.normal[rand.Intn(total)]
		}
	} else {
		if total := len(hero_id_set.advance) - 1; total >= 0 {
			award_hero_id = hero_id_set.advance[rand.Intn(total)]
		}
	}

	// 产生2个装备
	for i := 0; i < 2; i++ {
		index := randRoll([]uint8{60, 40})
		id := uint32(0)
		if index == 1 {
			if total := len(equip_id_set.normal) - 1; total >= 0 {
				id = equip_id_set.normal[rand.Intn(total)]
			}
		} else {
			if total := len(equip_id_set.advance) - 1; total >= 0 {
				id = equip_id_set.advance[rand.Intn(total)]
			}
		}

		award_item_ids = append(award_item_ids, id)
	}

	return award_hero_id, award_item_ids
}

func openTreasureBoxBlue() (uint32, []uint32) {
	award_hero_id := uint32(0)
	award_item_ids := []uint32{}
	index := 0

	// 产生角色卡
	index = randRoll([]uint8{70, 30})
	if index == 1 {
		if total := len(hero_id_set.advance) - 1; total >= 0 {
			award_hero_id = hero_id_set.advance[rand.Intn(total)]
		}
	} else {
		if total := len(hero_id_set.rare) - 1; total >= 0 {
			award_hero_id = hero_id_set.rare[rand.Intn(total)]
		}
	}

	// 产生2个装备
	for i := 0; i < 2; i++ {
		index := randRoll([]uint8{70, 30})
		id := uint32(0)
		if index == 1 {
			if total := len(equip_id_set.advance) - 1; total > 0 {
				id = equip_id_set.advance[rand.Intn(total)]
			}
		} else {
			if total := len(equip_id_set.rare) - 1; total > 0 {
				id = equip_id_set.rare[rand.Intn(total)]
			}
		}

		award_item_ids = append(award_item_ids, id)
	}

	return award_hero_id, award_item_ids
}

func openTreasureBoxGold() (uint32, []uint32) {
	/*
		award_hero_id := uint32(0)
		award_item_ids := []uint32{}
		index := 0

		// 产生角色卡
		index = randRoll([]uint8{80, 20})
		if index == 1 {
			if total := len(hero_id_set.rare) - 1; total > 0 {
				award_hero_id = hero_id_set.rare[rand.Intn(total)]
			}
		} else {
			if total := len(hero_id_set.legend) - 1; total > 0 {
				award_hero_id = hero_id_set.legend[rand.Intn(total)]
			}
		}

		// 产生2个装备
		for i := 0; i < 2; i++ {
			index := randRoll([]uint8{80, 20})
			id := uint32(0)
			if index == 1 {
				if total := len(equip_id_set.rare) - 1; total > 0 {
					id = equip_id_set.rare[rand.Intn(total)]
				}
			} else {
				if total := len(equip_id_set.legend) - 1; total > 0 {
					id = equip_id_set.legend[rand.Intn(total)]
				}
			}

			award_item_ids = append(award_item_ids, id)
		}

		return award_hero_id, award_item_ids
	*/
	award_hero_id := uint32(0)
	award_item_ids := []uint32{}
	index := 0

	// 产生角色卡
	index = randRoll([]uint8{50, 50})
	if index == 1 {
		if total := len(hero_id_set.advance) - 1; total > 0 {
			award_hero_id = hero_id_set.advance[rand.Intn(total)]
		}
	} else {
		if total := len(hero_id_set.rare) - 1; total > 0 {
			award_hero_id = hero_id_set.rare[rand.Intn(total)]
		}
	}

	// 产生2个装备
	for i := 0; i < 2; i++ {
		index := randRoll([]uint8{50, 50})
		id := uint32(0)
		if index == 1 {
			if total := len(equip_id_set.advance) - 1; total > 0 {
				id = equip_id_set.advance[rand.Intn(total)]
			}
		} else {
			if total := len(equip_id_set.rare) - 1; total > 0 {
				id = equip_id_set.rare[rand.Intn(total)]
			}
		}

		award_item_ids = append(award_item_ids, id)
	}

	return award_hero_id, award_item_ids
}

// 在黑市商店产生三个装备
func (s *Session) makeBlackStoreEquip() {
	equips := []uint32{}
	for _, e := range mapEquip {
		if e.is_show && !s.isInPack(e.item_id) {
			equips = append(equips, e.item_id)
		}
	}

	s.playerInfo.dbData.Game.BlackStoreItems = nil
	for i := 0; i < 3; i++ {
		if len(equips) <= 0 {
			break
		}
		index := rand.Intn(len(equips))
		id := equips[index]
		s.playerInfo.dbData.Game.BlackStoreItems = append(s.playerInfo.dbData.Game.BlackStoreItems, &pb.StoreItem{ItemInfo: &pb.Item{uint32(pb.ItemTypeEnum_ItemType_Equip), id, 0}})

		temp := []uint32{}
		for _, v := range equips {
			if id != v {
				temp = append(temp, v)
			}
		}
		equips = temp
	}
}

// 产生每日奖励
func (s *Session) makeDailyAward() {
	items := []*pb.DailyAwardItem{}
	items = append(items, &pb.DailyAwardItem{ItemInfo: &pb.Item{ItemType: uint32(pb.ItemTypeEnum_ItemType_Property), ItemId: uint32(pb.PropertyId_id_gold_coin), Value: int32(50)}})
	items = append(items, &pb.DailyAwardItem{ItemInfo: &pb.Item{ItemType: uint32(pb.ItemTypeEnum_ItemType_Property), ItemId: uint32(pb.PropertyId_id_gold_coin), Value: int32(100)}})
	items = append(items, &pb.DailyAwardItem{ItemInfo: &pb.Item{ItemType: uint32(pb.ItemTypeEnum_ItemType_Property), ItemId: uint32(pb.PropertyId_id_gold_coin), Value: int32(200)}})
	items = append(items, &pb.DailyAwardItem{ItemInfo: &pb.Item{ItemType: uint32(pb.ItemTypeEnum_ItemType_Treasure), ItemId: uint32(9059000)}})
	items = append(items, &pb.DailyAwardItem{ItemInfo: &pb.Item{ItemType: uint32(pb.ItemTypeEnum_ItemType_Treasure), ItemId: uint32(9059001)}})
	items = append(items, &pb.DailyAwardItem{ItemInfo: &pb.Item{ItemType: uint32(pb.ItemTypeEnum_ItemType_Treasure), ItemId: uint32(9059002)}})

	// 幸运转盘
	index := rand.Intn(len(items))
	s.playerInfo.dbData.Game.DailyAwardItems = []*pb.DailyAwardItem{}
	s.playerInfo.dbData.Game.DailyAwardItems = append(s.playerInfo.dbData.Game.DailyAwardItems, items[index])

	// 福利
	s.playerInfo.dbData.Game.DailyAwardItems = append(s.playerInfo.dbData.Game.DailyAwardItems, &pb.DailyAwardItem{ItemInfo: &pb.Item{ItemType: uint32(pb.ItemTypeEnum_ItemType_Property), ItemId: uint32(pb.PropertyId_id_gold_coin), Value: int32(50)}})
	s.playerInfo.dbData.Game.DailyAwardItems = append(s.playerInfo.dbData.Game.DailyAwardItems, &pb.DailyAwardItem{ItemInfo: &pb.Item{ItemType: uint32(pb.ItemTypeEnum_ItemType_Property), ItemId: uint32(pb.PropertyId_id_gold_coin), Value: int32(50)}})
	s.playerInfo.dbData.Game.DailyAwardItems = append(s.playerInfo.dbData.Game.DailyAwardItems, &pb.DailyAwardItem{ItemInfo: &pb.Item{ItemType: uint32(pb.ItemTypeEnum_ItemType_Property), ItemId: uint32(pb.PropertyId_id_gold_coin), Value: int32(50)}})
}

// 某个物品是否存在背包中
func (s *Session) isInPack(item_id uint32) bool {
	for _, v := range s.playerInfo.dbData.Game.Backpack {
		if item_id == v.ItemId && v.Total > 0 {
			return true
		}
	}
	return false
}

// 更新背包物品数量
func (s *Session) addItem(item_id uint32, num int32) bool {
	if mapItem[item_id] == nil && mapHero[item_id] == nil && mapEquip[item_id] == nil {
		return false
	}

	for _, v := range s.playerInfo.dbData.Game.Backpack {
		if item_id == v.ItemId {
			if v.Total+num <= 0 {
				v.Total = 0
			} else {
				v.Total = v.Total + num
			}
			return true
		}
	}
	s.playerInfo.dbData.Game.Backpack = append(s.playerInfo.dbData.Game.Backpack, &pb.PackGrid{ItemId: item_id, Total: num})

	return true
}

// 更新玩家属性
func (s *Session) addProperty(property_id uint32, num int32) bool {
	if property_id < uint32(pb.PropertyId_PropertyId_Max) {
		s.playerInfo.dbData.Game.PlayerProperties[property_id] = s.playerInfo.dbData.Game.PlayerProperties[property_id] + num
	}
	return true
}

// 是否拥有英雄
func (s *Session) hadHero(hero_id uint32) bool {
	for _, v := range s.playerInfo.dbData.Game.Heroes {
		if hero_id == v.HeroId {
			return true
		}
	}
	return false
}

// 添加一个英雄
func (s *Session) addHero(hero_id uint32) bool {
	s.playerInfo.dbData.Game.Heroes = append(s.playerInfo.dbData.Game.Heroes, &pb.Hero{HeroId: hero_id, Level: 1})
	return true
}

// 找到黑市商品
func (s *Session) getBlackItem(item_id uint32) *pb.StoreItem {
	for _, v := range s.playerInfo.dbData.Game.BlackStoreItems {
		//fmt.Println("black store item_id =", v.ItemInfo.ItemId)
		if item_id == v.ItemInfo.ItemId {
			return v
		}
	}
	return nil
}
