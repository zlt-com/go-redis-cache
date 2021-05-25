package redcache

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

type RedCache struct {
	// StructFields *StructField
	Conditions Condition
}

func parseTagSetting(tags reflect.StructTag) map[string]string {
	setting := map[string]string{}
	for _, str := range []string{tags.Get("redcache")} {
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

	result := common.ReflectMethod(i, "TableName")
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
				hasValueTag := false
				for _, v := range tags {
					if v == "VALUE" {
						hasValueTag = true
					}
				}
				if hasValueTag {
					sf.Index[fieldName] = sf.TableName + "_index_" + fieldName + "_" + refValue.Field(i).Interface().(string)
				} else {
					sf.Index[fieldName] = sf.TableName + "_index_" + fieldName
				}

				if refValue.Kind() == reflect.Ptr || refValue.Kind() == reflect.Slice {
					sf.KV[fieldName] = refValue.Elem().Field(i).Interface()
				} else {
					sf.KV[fieldName] = refValue.Field(i).Interface()
				}
			}
			// sf.Values = append(sf.Values, refValue.Elem().Field(i).Interface())
		}
		// fmt.Printf("%6s: %v = %v\n", f.Name, f.Type, val)
	}
	return
}

// Select
func (rc *RedCache) Select() (reply []interface{}, count int, err error) {
	sf := getStructField(rc.Conditions.Model)
	if !redisDb.Exists(sf.TableName) {
		return nil, 0, nil
	}
	index := ""
	indexValue := make([]interface{}, 0)
	for key, value := range sf.Index {
		for whereKey, whereValue := range rc.Conditions.Where {
			if whereKey == key {
				index = value
				if iv, err := selectIndex(index, whereValue); err != nil || iv == nil {
					return nil, 0, err
				} else {
					indexValue = append(indexValue, redisDb.String(iv))
				}
				if tag := sf.Tags[whereKey]; tag != nil && len(indexValue) > 0 {
					for _, tv := range tag {
						if tv == "MUILT_INDEX" {
							// array := make([]interface{}, 0)
							_, err = common.JSON2Object(indexValue[0].(string), &indexValue)
						}
					}
				}
			}

		}
	}

	if len(indexValue) == 0 {
		if iv, err := selectRangeIndex(sf.TableName+"_id", rc.Conditions.Offset, rc.Conditions.Limit); err != nil || iv == nil {
			return nil, 0, err
		} else {
			indexValue = append(indexValue, iv...)
		}
	}
	if len(indexValue) == 0 {
		return
	}

	// if indexValue, err := selectIndex(index, rc.Conditions.Value); err == nil && indexValue != nil {
	// 	if reply, err = redisDb.Hget(sf.TableName, redisDb.String(indexValue)); err == nil && reply != nil {
	// 		reply = redisDb.String(reply)
	// 	}
	// }
	mgetField := []interface{}{sf.TableName}
	// mgetField = append(mgetField, sf.TableName)
	mgetField = append(mgetField, indexValue...)
	if mgetValues, err := redisDb.Hmget(mgetField...); err != nil {
		// reply = redisDb.String(reply)
		fmt.Println(err)
	} else {
		for _, replyValue := range mgetValues {
			reply = append(reply, redisDb.String(replyValue))
		}
	}

	return
}

func selectIndex(index string, key interface{}) (reply interface{}, err error) {
	return redisDb.Hget(index, key)
}

func selectRangeIndex(index string, start, end interface{}) ([]interface{}, error) {
	// fmt.Println("zrevrange", index, start, start.(int)+end.(int))
	return redisDb.Zrevrange(index, start, start.(int)+end.(int))
}

func (rc *RedCache) Delete() (err error) {
	switch value := rc.Conditions.Instance.(type) {
	case string:
	case int:
	case map[interface{}]interface{}:

	default:
		sf := getStructField(value)

		if err = redisDb.Hdel(sf.TableName, sf.KV["id"]); err != nil {
			fmt.Println(err)
		}
		if err = deleteIndex(sf); err != nil {
			fmt.Println(err)
		}
	}
	return
}

func (rc *RedCache) Create() (err error) {
	switch value := rc.Conditions.Instance.(type) {
	case string:
	case int:
	case map[interface{}]interface{}:

	default:
		sf := getStructField(value)
		if exists, err := redisDb.Hexists(sf.TableName, sf.KV["id"]); err == nil {
			if !exists {
				if err = redisDb.Hset(sf.TableName, sf.KV["id"], common.Object2JSON(rc.Conditions.Instance)); err == nil {
					if err = createIndex(sf, rc.Conditions.Instance); err != nil {
						return err
					}
				}

			}
		}

	}
	return
}

func createIndex(sf *StructField, i interface{}) (err error) {
	for k, v := range sf.KV {
		if k == "id" {
			if err := redisDb.Zadd(sf.TableName+"_id", v.(int), v); err != nil {
				fmt.Println(err)
			}
			// continue
		}

		if tag := sf.Tags[k]; tag != nil {
			for _, tv := range tag {
				if tv == "UNIQUE_INDEX" {
					redisDb.Hset(sf.Index[k], v, sf.KV["id"])
				} else if tv == "MUILT_INDEX" {
					muiltValue := make([]interface{}, 0)
					if ok, _ := redisDb.Hexists(sf.Index[k], v); ok {
						if indexV, err := redisDb.Hget(sf.Index[k], v); err != nil {
							fmt.Println(err)
						} else {
							arry := make([]interface{}, 0)
							if _, err := common.JSON2Object(redisDb.String(indexV), &arry); err != nil {
								fmt.Println(err)
								return err
							} else {
								muiltValue = append(muiltValue, arry...)
							}
						}
					}
					muiltValue = append(muiltValue, common.ReflectFilde(i, "ID"))
					redisDb.Hset(sf.Index[k], v, common.Object2JSON(muiltValue))
				}
			}
		}
	}
	return
}

func deleteIndex(sf *StructField) (err error) {
	for k, v := range sf.KV {
		if k == "id" {
			if err := redisDb.Zrem(sf.TableName+"_id", v); err != nil {
				fmt.Println(err)
			}
			// continue
		}

		if tag := sf.Tags[k]; tag != nil {
			for _, tv := range tag {
				if tv == "UNIQUE_INDEX" {
					redisDb.Hdel(sf.Index[k], v)
				} else if tv == "MUILT_INDEX" {
					if ok, _ := redisDb.Hexists(sf.Index[k], k); ok {
						if cacheValue, err := redisDb.Hget(sf.Index[k], k); err != nil {
							fmt.Println(err)
						} else {
							cacheValueArray := make([]interface{}, 0)
							_, err = common.JSON2Object(redisDb.String(cacheValue), cacheValueArray)
							if err != nil {
								return err
							}
							delIndex := -1
							for index, value := range cacheValueArray {
								if value == v {
									delIndex = index
								}
							}
							cacheValueArray = append(cacheValueArray[:delIndex], cacheValueArray[delIndex+1:]...)
							err = redisDb.Hset(sf.Index[k], k, common.Object2JSON(cacheValueArray))
							if err != nil {
								return err
							}
						}
					}

				}
			}
		}
	}
	return
}

func (rc *RedCache) Count() (count int, err error) {
	switch value := rc.Conditions.Model.(type) {
	default:
		sf := getStructField(value)
		if rc.Conditions.Where != nil {
			indexValue := make([]interface{}, 0)
			for key := range sf.Index {
				for whereKey := range rc.Conditions.Where {
					if whereKey == key {
						if tag := sf.Tags[whereKey]; tag != nil && len(indexValue) > 0 {
							for _, tv := range tag {
								if tv == "MUILT_INDEX" {
									// array := make([]interface{}, 0)
									_, err = common.JSON2Object(indexValue[0].(string), &indexValue)
								}
							}
						}
					}

				}
			}
			count = len(indexValue)
		} else {
			if count, err = redisDb.Zcard(sf.TableName + "_id"); err != nil {
				fmt.Println(err)
			}
		}
	}
	return
}
