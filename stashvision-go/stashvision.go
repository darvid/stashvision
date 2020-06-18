package stashvision

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/blevesearch/bleve"
	"github.com/blevesearch/bleve/index/scorch"
	"github.com/carlescere/scheduler"
	"github.com/imroc/req"
	"github.com/mitchellh/mapstructure"
	"github.com/shibukawa/configdir"
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

var ConfigDirs configdir.ConfigDir
var limiter = rate.NewLimiter(rate.Every(time.Minute/6), 3)

const (
	vendorName string = "darvid"
	appName    string = "stashvision"
	Version    string = "0.1.0"
)

const (
	bleveIndexNameStash string = "stash_tabs.bleve"
	poeBaseUrl          string = "https://www.pathofexile.com/"
	poeGetStashItems    string = "/character-window/get-stash-items"
)

const PoeQuadTabSize = 24 * 24

func poeApiPath(pathname string) string {
	poeUrl, _ := url.Parse(poeBaseUrl)
	poeUrl.Path = path.Join(poeUrl.Path, poeGetStashItems)
	return poeUrl.String()
}

const (
	PoeValueTypeWhite     = 0
	PoeValueTypeBlue      = 1
	PoeValueTypeFire      = 4
	PoeValueTypeCold      = 5
	PoeValueTypeLightning = 6
	PoeValueTypeChaos     = 7
)

const (
	PoeFrameTypeNormal    = iota
	PoeFrameTypeMagic     = iota
	PoeFrameTypeRare      = iota
	PoeFrameTypeUnique    = iota
	PoeFrameTypeGem       = iota
	PoeFrameTypeCurrency  = iota
	PoeFrameTypeDivCard   = iota
	PoeFrameTypeQuestItem = iota
	PoeFrameTypeProphecy  = iota
	PoeFrameTypeRelic     = iota
)

var poeValueTypes = map[int]string{
	PoeValueTypeWhite:     "white",
	PoeValueTypeBlue:      "blue",
	PoeValueTypeFire:      "fire",
	PoeValueTypeCold:      "cold",
	PoeValueTypeLightning: "lightning",
	PoeValueTypeChaos:     "chaos",
}

var poeFrameTypes = map[int]string{
	PoeFrameTypeNormal:    "normal",
	PoeFrameTypeMagic:     "magic",
	PoeFrameTypeRare:      "rare",
	PoeFrameTypeUnique:    "unique",
	PoeFrameTypeGem:       "gem",
	PoeFrameTypeCurrency:  "currency",
	PoeFrameTypeDivCard:   "divination_card",
	PoeFrameTypeQuestItem: "quest_item",
	PoeFrameTypeProphecy:  "prophecy",
	PoeFrameTypeRelic:     "relic",
}

type PoeItemValue struct {
	Value          string  `json:"value"`
	EffectiveValue float64 `json:"effectiveValue"`
	ValueType      int     `json:"valueType"`
}

type PoeItemProperty struct {
	DisplayMode int            `json:"displayMode"`
	Name        string         `json:"name"`
	Type        int            `json:"type"`
	Values      []PoeItemValue `json:"values"`
}

type PoeStashItem struct {
	Class       string            `json:"class"`
	Corrupted   int8              `json:"corrupted"`
	FrameType   string            `json:"frameType"`
	Height      int               `json:"h"`
	Icon        string            `json:"icon"`
	ID          string            `json:"id"`
	Identified  int8              `json:"identified"`
	ItemLevel   int               `json:"ilvl"`
	InventoryId string            `json:"inventoryId"`
	League      string            `json:"league"`
	Name        string            `json:"name"`
	NumLinks    int               `json:"numLinks"`
	NumSockets  int               `json:"numSockets"`
	Properties  []PoeItemProperty `json:"properties"`
	TabIndex    int               `json:"tabIndex"`
	TypeLine    string            `json:"typeLine"`
	Verified    int8              `json:"verified"`
	Width       int               `json:"w"`
	X           int               `json:"x"`
	Y           int               `json:"y"`
}

func (i *PoeStashItem) ToString() string {
	name := i.Name
	if name == "" {
		name = "[unid]"
	}
	return fmt.Sprintf("%d %s %s", i.ItemLevel, name, i.TypeLine)
}

func (i *PoeStashItem) PositionString() string {
	return fmt.Sprintf("%dx%d:%d,%d", i.Width, i.Height, i.X, i.Y)
}

var poeStashItemBoolFields []string = []string{
	"corrupted",
	"identified",
	"verified",
}

func init() {
	ConfigDirs = configdir.New(vendorName, appName)
	ConfigDirs.LocalPath, _ = filepath.Abs(filepath.Dir(os.Args[0]))
}

func CreateOrOpenIndex(runtimeConfig *map[string]interface{}) (bleve.Index, error) {
	// XXX: Need to detect read-only attribute on index paths to ensure
	// bleve/scorch can delete index files successfully, or warn the user
	// if it doesn't have permission.

	// cache := ConfigDirs.QueryCacheFolder()
	// cache.MkdirAll()
	// indexPath := filepath.Join(cache.Path, bleveIndexNameStash)

	indexPath := filepath.Join(ConfigDirs.LocalPath, bleveIndexNameStash)
	ctxLogger := log.WithFields(log.Fields{
		"indexName": bleveIndexNameStash,
		"indexPath": indexPath,
	})
	if _, err := os.Stat(indexPath); !os.IsNotExist(err) {
		ctxLogger.Debug("opening index")
		if runtimeConfig != nil {
			return bleve.OpenUsing(indexPath, *runtimeConfig)
		} else {
			return bleve.Open(indexPath)
		}
	}
	ctxLogger.Debug("creating index")
	indexMapping := bleve.NewIndexMapping()
	index, err := bleve.NewUsing(indexPath, indexMapping, scorch.Name, scorch.Name, nil)
	if err != nil {
		return nil, err
	}
	return index, nil
}

func ClearIndexFromQuery(querystring string, index bleve.Index) error {
	ctxLogger := log.WithFields(log.Fields{"querystring": querystring})
	ctxLogger.Debug("clearing index")
	items, err := QueryIndex(querystring, index, PoeQuadTabSize)
	if err != nil {
		ctxLogger.Error(err)
		return errors.New("query failed")
	}
	batch := index.NewBatch()
	for _, item := range items {
		batch.Delete(item.ID)
	}
	err = index.Batch(batch)
	if err != nil {
		ctxLogger.Error(err)
		return errors.New("failed to clear index")
	}
	ctxLogger.Debug("index cleared")
	return nil
}

func IndexStashItem(item PoeStashItem, batch bleve.Batch) error {
	ctxLogger := log.WithFields(log.Fields{"item": item.ToString()})
	err := batch.Index(item.ID, item)
	if err != nil {
		return errors.New("failed to index stash item")
	}
	doc, err := json.Marshal(item)
	if err != nil {
		return errors.New("failed to encode stash item as JSON")
	}
	batch.SetInternal([]byte(item.ID), doc)
	ctxLogger.Debug("indexed stash item")
	return nil
}

func IndexStashItems(items []PoeStashItem, index bleve.Index, clearTabIndex int) error {
	var err error
	if clearTabIndex != -1 {
		err = ClearIndexFromQuery(fmt.Sprintf("tabIndex:%d", clearTabIndex), index)
		if err != nil {
			return err
		}
	}
	batch := index.NewBatch()
	for _, item := range items {
		err = IndexStashItem(item, *batch)
		if err != nil {
			log.Error(err)
		}
	}
	err = index.Batch(batch)
	if err != nil {
		log.Error(err)
		return err
	}
	return nil
}

func RunServer(poeSessionId string, accountName string, leagueName string, tabIndex int, index *bleve.Index) {
	session := PoeSession{poeSessionId}

	job := func() {
		cleanup := index == nil
		if index == nil {
			if index_, err := CreateOrOpenIndex(nil); err != nil {
				log.Error(err)
				return
			} else {
				index = &index_
			}
		}
		items, err := session.GetStashItems(accountName, leagueName, 0, tabIndex)
		if err != nil {
			log.Error(err)
			return
		}
		err = IndexStashItems(items, *index, tabIndex)
		if cleanup {
			(*index).Close()
			index = nil
		}
		if err != nil {
			log.Fatalln("failed to batch index stash items")
		}
		return
	}
	scheduler.Every(10).Seconds().Run(job)
	runtime.Goexit()
}

func QueryIndex(querystring string, index bleve.Index, searchSize int) (items []PoeStashItem, err error) {
	query := bleve.NewQueryStringQuery(querystring)
	search := bleve.NewSearchRequest(query)
	search.Size = searchSize
	search.SortBy([]string{"x", "y"})
	searchResults, err := index.Search(search)
	if err != nil {
		return
	}
	log.Debug(searchResults)
	for _, hit := range searchResults.Hits {
		id := hit.ID
		raw, err := index.GetInternal([]byte(id))
		if err != nil {
			return items, err
		}
		var item PoeStashItem
		err = json.Unmarshal(raw, &item)
		if err != nil {
			return items, errors.New("failed to decode stash item JSON")
		}
		items = append(items, item)
	}
	return
}

type PoeSession struct {
	sessionId string
}

func (s *PoeSession) request(path string, param req.Param) (r *req.Resp, err error) {
	ctxLogger := log.WithFields(log.Fields{
		"path": path,
	})
	if limiter.Allow() == false {
		ctxLogger.Debug("Rate limit exceeded")
		err = errors.New("rate limit exceeded")
		return
	}
	header := req.Header{
		"Cookie": fmt.Sprintf("POESESSID=%s", s.sessionId),
	}
	ctxLogger.Info("request started")
	r, err = req.Get(poeApiPath(path), header, param)
	if err != nil {
		ctxLogger.Error(err)
		return
	}
	ctxLogger.WithFields(log.Fields{
		"cost":   r.Cost(),
		"length": len(r.Bytes()),
	}).Info("request complete")
	return
}

func (s *PoeSession) GetStashItems(accountName string, league string, tabs int, tabIndex int) (items []PoeStashItem, err error) {
	ctxLogger := log.WithFields(log.Fields{
		"accountName": accountName,
		"league":      league,
		"tabs":        tabs,
		"tabIndex":    tabIndex,
	})
	ctxLogger.Info("getting stash items")
	param := req.Param{
		"accountName": accountName,
		"league":      league,
		"tabIndex":    tabIndex,
		"realm":       "pc",
	}
	r, err := s.request(poeGetStashItems, param)
	if err != nil {
		ctxLogger.Error(err)
		return
	}
	var v map[string]interface{}
	err = r.ToJSON(&v)
	if err != nil {
		ctxLogger.Error(err)
		return
	}
	if _, ok := v["error"]; ok {
		respErr := v["error"].(map[string]interface{})
		errCode := int(respErr["code"].(float64))
		errMessage := respErr["message"]
		ctxLogger.WithFields(log.Fields{
			"errorCode":    errCode,
			"errorMessage": errMessage,
		}).Error("got error response from API")
		return
	}
	hookFunc := func(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
		switch t {
		case reflect.TypeOf(PoeItemValue{}):
			field := data.([]interface{})
			value := field[0].(string)
			valueType := int(field[1].(float64))
			if strings.HasSuffix(value, "%") {
				effectiveValue, err := strconv.ParseFloat(value[:len(value)-1], 32)
				if err != nil {
					return data, nil
				} else {
					effectiveValue /= 100
				}
				return PoeItemValue{
					EffectiveValue: effectiveValue,
					Value:          value,
					ValueType:      valueType,
				}, nil
			}
		case reflect.TypeOf(PoeStashItem{}):
			field := data.(map[string]interface{})
			class := field["typeLine"].(string)
			if strings.HasPrefix(class, "Superior ") {
				class = class[9:]
			}
			field["class"] = poeItemNamesToClasses[class]
			field["tabIndex"] = tabIndex
			frameType := int(field["frameType"].(float64))
			field["frameType"] = poeFrameTypes[frameType]
			if sockets, ok := field["sockets"].([]interface{}); ok {
				field["numSockets"] = len(sockets)
				numLinks := 0
				groups := make(map[int]int)
				for _, socket := range sockets {
					groupID := int(socket.(map[string]interface{})["group"].(float64))
					if _, ok := groups[groupID]; !ok {
						groups[groupID] = 0
					}
					groups[groupID]++
					if groups[groupID] > numLinks {
						numLinks = groups[groupID]
					}
				}
				field["numLinks"] = numLinks
			}
			for _, boolField := range poeStashItemBoolFields {
				if field[boolField] != nil && field[boolField].(bool) {
					field[boolField] = 1
				} else {
					field[boolField] = 0
				}
			}
		}
		return data, nil
	}
	decoder, _ := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: hookFunc,
		Metadata:   nil,
		Result:     &items,
		TagName:    "json",
	})
	decoder.Decode(v["items"])
	ctxLogger.WithFields(log.Fields{
		"numItems": len(items),
	}).Info("stash tab downloaded")
	return
}
