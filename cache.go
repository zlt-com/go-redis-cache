package rediscache

import (
	"fmt"

	"github.com/zlt-com/go-common"
	database "github.com/zlt-com/go-db"
)

var redisDb database.RedisDB

func init() {
	database.Open([]string{"mysql", "redis"})
	redisDb = database.RedisDB{DBNum: 0}
	// info, err := redisDb.Info()
	// fmt.Println(string(info.([]byte)[:]), err)
}

// Select
func Select() {

}

func Create(key string, object interface{}) {
	object = common.Object2JSON(object)
	fmt.Println(object)
}
