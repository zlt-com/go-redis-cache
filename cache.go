package redcache

import (
	"fmt"
	"go/ast"
	"reflect"
	"strings"

	"github.com/zlt-com/go-common"
	database "github.com/zlt-com/go-db"
)

var (
	//存储缓存数据加载情况，数据库和缓存数据量一致为真
	SyncCache = make(map[string]bool)
	//是否使用缓存
	UseRedcache = true
	//redis数据库实例，默认是0号
	redisDb = database.RedisDB{DBNum: 0}
)

type StructField struct {
	TableName string
	Name      string
	Tags      map[string]map[string]string
	Index     map[string]string
	IndexKv   map[string]interface{}
}

type RedCache struct {
	model interface{}
	// instance interface{}
	offset  interface{}
	limit   interface{}
	orderBy interface{}
	where   map[string]interface{}
}

func New() (redCache *RedCache) {
	redCache = new(RedCache)
	return
}

func (rc *RedCache) Model(model interface{}) *RedCache {
	rc.model = model
	return rc
}

// func (rc *RedCache) Instance(instance interface{}) *RedCache {
// 	rc.instance = instance
// 	return rc
// }

func (rc *RedCache) Offset(offset interface{}) *RedCache {
	rc.offset = offset
	return rc
}

func (rc *RedCache) Limit(limit interface{}) *RedCache {
	rc.limit = limit
	return rc
}

func (rc *RedCache) OrderBy(orderBy interface{}) *RedCache {
	rc.orderBy = orderBy
	return rc
}

func (rc *RedCache) Where(where map[string]interface{}) *RedCache {
	rc.where = where
	return rc
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

var structFieldMap = make(map[string]*StructField)

func getStructField(m interface{}) (sf *StructField) {
	reflectType := reflect.ValueOf(m).Type()
	refValue := reflect.ValueOf(m)
	for reflectType.Kind() == reflect.Slice || reflectType.Kind() == reflect.Ptr {
		reflectType = reflectType.Elem()
	}
	modelName := reflectType.Name()
	if structFieldMap[modelName] != nil {
		return structFieldMap[modelName]
	}
	sf = new(StructField)

	result := common.ReflectMethod(m, "TableName")
	sf.TableName = result[0].String()
	sf.Tags = make(map[string]map[string]string)
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
			}
		}
		// fmt.Printf("%6s: %v = %v\n", f.Name, f.Type, val)
	}
	structFieldMap[modelName] = sf
	return
}

// Select
func (rc *RedCache) Select() (reply []interface{}, err error) {
	sf := getStructField(rc.model)
	//没有设置缓存字段
	if len(sf.Index) == 0 {
		return nil, nil
	}
	if b, err := redisDb.Exists(sf.TableName); !b || err != nil {
		return nil, nil
	}
	index := ""
	indexValue := make([]interface{}, 0)
	for key, value := range sf.Index {
		for whereKey, whereValue := range rc.where {
			if whereKey == key {
				index = value
				if iv, err := selectIndex(index, whereValue); err != nil || iv == nil {
					return nil, err
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
		if iv, err := selectRangeIndex(sf.TableName+"_id", rc.offset, rc.limit); err != nil || iv == nil {
			return nil, err
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
	switch value := rc.model.(type) {
	case string:
	case int:
	case map[interface{}]interface{}:

	default:
		sf := getStructField(value)

		if err = redisDb.Hdel(sf.TableName, sf.IndexKv["id"]); err != nil {
			fmt.Println(err)
		}
		if err = deleteIndex(sf); err != nil {
			fmt.Println(err)
		}
	}
	return
}

func (rc *RedCache) Create() (err error) {
	switch value := rc.model.(type) {
	case string:
	case int:
	case map[interface{}]interface{}:

	default:
		sf := getStructField(value)
		key := common.ReflectFilde(value, "ID")
		if exists, err := redisDb.Exists(sf.TableName); err == nil {
			if !exists {
				if err = redisDb.Hset(sf.TableName, key, common.Object2JSON(value)); err == nil {
					if err = createIndex(sf, rc.model); err != nil {
						return err
					}
				}

			}
		}

	}
	return
}

func (rc *RedCache) BatchCreate(i []interface{}) (err error) {
	commandArgs := []interface{}{}
	instances := []interface{}{}
	sf := new(StructField)

	for i, value := range i {
		sf = getStructField(value)
		if i == 0 {
			commandArgs = append(commandArgs, sf.TableName)
		}
		key := common.ReflectFilde(value, "ID")
		commandArgs = append(commandArgs, key, common.Object2JSON(value))
		instances = append(instances, value)
	}
	if err = redisDb.Hmset(commandArgs...); err == nil {
		for index := 0; index < len(instances); index++ {
			if err = createIndex(sf, instances[index]); err != nil {
				return err
			}
		}
	}
	return
}

func createIndex(sf *StructField, i interface{}) (err error) {
	sf.IndexKv = common.ReflectFildes(i)
	for k, v := range sf.IndexKv {
		if k == "id" {
			if err := redisDb.Zadd(sf.TableName+"_id", v.(int), v); err != nil {
				fmt.Println(err)
			}
			// continue
		}

		if tag := sf.Tags[k]; tag != nil {
			for _, tv := range tag {
				if tv == "UNIQUE_INDEX" {
					redisDb.Hset(sf.Index[k], v, sf.IndexKv["id"])
				} else if tv == "MUILT_INDEX" {
					muiltValue := make([]int, 0)
					if ok, _ := redisDb.Hexists(sf.Index[k], v); ok {
						if indexV, err := redisDb.Hget(sf.Index[k], v); err != nil {
							fmt.Println(err)
						} else {
							arry := make([]int, 0)
							if _, err := common.JSON2Object(redisDb.String(indexV), &arry); err != nil {
								fmt.Println(err)
								return err
							} else {
								muiltValue = append(muiltValue, arry...)
							}
						}
					}
					id := common.ReflectFilde(i, "ID")
					if !common.Contains(id, muiltValue) {
						muiltValue = append(muiltValue, common.ReflectFilde(i, "ID").(int))
					}
					redisDb.Hset(sf.Index[k], v, common.Object2JSON(muiltValue))
				}
			}
		}
	}
	return
}

func deleteIndex(sf *StructField) (err error) {
	for k, v := range sf.IndexKv {
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
	switch value := rc.model.(type) {
	default:
		sf := getStructField(value)
		if rc.where != nil && len(rc.where) > 0 {
			indexValue := make([]interface{}, 0)
			for whereKey, whereValue := range rc.where {
				key := sf.Index[whereKey]
				if key != "" {
					if tag := sf.Tags[whereKey]; tag != nil {
						for _, tv := range tag {
							if tv == "MUILT_INDEX" {
								// array := make([]interface{}, 0)
								if reply, err := redisDb.Hget(key, whereValue); err != nil || reply == nil {
									return 0, err
								} else {
									if _, err = common.JSON2Object(redisDb.String(reply), &indexValue); err != nil {
										return 0, err
									}
								}
							} else {
								return 1, err
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

var (
	syncstatus = "syncstatus"
)

//设置同步状态
func (rc *RedCache) SetSyncStatus(status map[string]bool) (reply interface{}, err error) {
	json := common.Object2JSON(status)
	return redisDb.Set(syncstatus, json)
}

func (rc *RedCache) GetSyncStatus() (status map[string]bool, err error) {
	status = make(map[string]bool)
	if ex, err := redisDb.Exists(syncstatus); ex && err == nil {
		if statusValue, err := redisDb.Get(syncstatus); err == nil {
			_, err = common.JSON2Object(statusValue.(string), &status)
			return status, err
		} else {
			return make(map[string]bool), nil
		}
	} else {
		return make(map[string]bool), nil
	}
}
