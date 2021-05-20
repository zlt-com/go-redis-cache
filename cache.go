package rediscache

import (
	"fmt"
	"go/ast"
	"reflect"
	"strings"

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

type StructField struct {
	TableName string
	Name      string
	KV        map[string]interface{}
	Tags      map[string]map[string]string
	Index     map[string]string
}

type RedisCache struct {
	// StructFields *StructField
	Conditions Condition
}

func parseTagSetting(tags reflect.StructTag) map[string]string {
	setting := map[string]string{}
	for _, str := range []string{tags.Get("rcache")} {
		if str == "" {
			continue
		}
		tags := strings.Split(str, ";")
		for _, value := range tags {
			v := strings.Split(value, ":")
			k := strings.TrimSpace(strings.ToUpper(v[0]))
			if len(v) >= 2 {
				setting[k] = strings.Join(v[1:], ":")
			} else {
				setting[k] = k
			}
		}
	}
	return setting
}

func getStructField(i interface{}) (sf *StructField) {
	reflectType := reflect.ValueOf(i).Type()
	refValue := reflect.ValueOf(i)
	for reflectType.Kind() == reflect.Slice || reflectType.Kind() == reflect.Ptr {
		reflectType = reflectType.Elem()
	}

	sf = new(StructField)

	method := refValue.MethodByName("TableName")
	result := method.Call(nil)
	sf.TableName = result[0].String()

	sf.Tags = make(map[string]map[string]string)
	sf.KV = make(map[string]interface{})
	sf.Index = make(map[string]string)
	for i := 0; i < reflectType.NumField(); i++ {
		if fieldStruct := reflectType.Field(i); ast.IsExported(fieldStruct.Name) {
			tags := parseTagSetting(fieldStruct.Tag)
			fieldName := strings.ToLower(fieldStruct.Name)
			if len(tags) > 0 {
				sf.Tags[fieldName] = tags
				sf.Index[fieldName] = sf.TableName + "_index_" + fieldName
				sf.KV[fieldName] = refValue.Elem().Field(i).Interface()
			}

			// sf.Values = append(sf.Values, refValue.Elem().Field(i).Interface())
		}
		// fmt.Printf("%6s: %v = %v\n", f.Name, f.Type, val)
	}

	return
}

// Select
func (rc *RedisCache) Select() (reply interface{}, err error) {
	sf := getStructField(rc.Conditions.Model)
	index := ""
	for key, value := range sf.Index {
		if rc.Conditions.Field == key {
			index = value
		}
	}
	if indexValue, err := selectIndex(index, rc.Conditions.Value); err == nil {
		if reply, err = redisDb.Hget(sf.TableName, redisDb.String(indexValue)); err == nil && reply != nil {
			reply = redisDb.String(reply)
		}
	}

	return
}

func selectIndex(index string, key interface{}) (reply interface{}, err error) {
	return redisDb.Hget(index, key)
}

func (rc *RedisCache) Create() (err error) {
	switch value := rc.Conditions.Value.(type) {
	case string:
	case int:
	case map[interface{}]interface{}:

	default:
		sf := getStructField(value)
		if err = redisDb.Hset(sf.TableName, sf.KV["id"], common.Object2JSON(rc.Conditions.Value)); err != nil {
			fmt.Println(err)
		}
		if err = createIndex(sf); err != nil {
			fmt.Println(err)
		}
	}
	return
}

func createIndex(sf *StructField) (err error) {
	for k, v := range sf.KV {
		if k == "id" {
			if err := redisDb.Zadd(sf.Index[k], v.(int), v); err != nil {
				fmt.Println(err)
			}
			continue
		}

		if tag := sf.Tags[k]; tag != nil {
			for _, tv := range tag {
				if tv == "UNIQUE_INDEX" {
					redisDb.Hset(sf.Index[k], v, sf.KV["id"])
				} else if tv == "MUILT_INDEX" {
					muiltValue := make([]interface{}, 0)
					if ok, _ := redisDb.Hexists(sf.Index[k], k); ok {
						if v, err = redisDb.Hget(sf.Index[k], k); err != nil {
							fmt.Println(err)
						} else {
							arry := make([]interface{}, 0)
							muiltValue = append(muiltValue, common.JSON2Object(redisDb.String(v), arry))
						}
					}
					muiltValue = append(muiltValue, v)
					redisDb.Hset(sf.Index[k], k, common.Object2JSON(muiltValue))
				}
			}
		}
	}
	return
}
