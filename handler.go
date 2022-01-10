package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-redis/redis/v8"
	"github.com/gorilla/mux"
	"github.com/tidwall/gjson"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"
)

var (
	currencyCodeDefault  = []string{"KRW","USD","IDR","SGD","THB"}

)
const(
	coinBaseApi string = "https://api.coinbase.com/v2/exchange-rates"
	coinMarketApi string = "https://pro-api.coinmarketcap.com/v1/cryptocurrency/quotes/latest"
	coinGeckoApi string = "https://api.coingecko.com/api/v3/coins/markets"
	symbolIdApi string = "https://api.coingecko.com/api/v3/coins/list"
)

type RequestBody struct {
	CurrencyCode []string
}
type CoinGeckoMarket struct {
	Id                           string      `json:"id"`
	Symbol                       string      `json:"symbol"`
	Name                         string      `json:"name"`
	Image                        string      `json:"image"`
	CurrentPrice                 float64        `json:"current_price"`
	MarketCap                    float64       `json:"market_cap"`
	MarketCapRank                float64        `json:"market_cap_rank"`
	FullyDilutedValuation        float64      `json:"fully_diluted_valuation"`
	TotalVolume                  float64       `json:"total_volume"`
	High24H                      float64       `json:"high_24h"`
	Low24H                       float64        `json:"low_24h"`
	PriceChange24H               float64     `json:"price_change_24h"`
	PriceChangePercentage24H     float64     `json:"price_change_percentage_24h"`
	MarketCapChange24H           float64     `json:"market_cap_change_24h"`
	MarketCapChangePercentage24H float64     `json:"market_cap_change_percentage_24h"`
	CirculatingSupply            float64     `json:"circulating_supply"`
	TotalSupply                  float64     `json:"total_supply"`
	MaxSupply                    interface{}     `json:"max_supply"`
	Ath                          float64        `json:"ath"`
	AthChangePercentage          float64     `json:"ath_change_percentage"`
	AthDate                      time.Time   `json:"ath_date"`
	Atl                          float64     `json:"atl"`
	AtlChangePercentage          float64     `json:"atl_change_percentage"`
	AtlDate                      time.Time   `json:"atl_date"`
	Roi                          interface{} `json:"roi"`
	LastUpdated                  time.Time   `json:"last_updated"`
}

type SymbolId struct {
	ObjectId string `json:"_id"`
	Id string `json:"id"`
	Symbol string `json:"symbol"`
	Name string `json:"name"`
}

type errResult struct {
	Code int `json:"code"`
	Msg  string `json:"msg"`
}
type Data struct {
	Symbol               string `json:"symbol"`
	CurrencyCode         string `json:"currencyCode"`
	Price                float64 `json:"price"`
	MarketCap            float64 `json:"marketCap"`
	AccTradePrice24H     interface{} `json:"accTradePrice24h"`
	CirculatingSupply    float64 `json:"circulatingSupply"`
	MaxSupply            interface{} `json:"maxSupply"`
	Provider             string `json:"provider"`
	LastUpdatedTimestamp string `json:"lastUpdatedTimestamp"`
}
type Redis struct {
	MarketCap            float64 `json:"marketCap"`
	CirculatingSupply    float64 `json:"circulatingSupply"`
	MaxSupply            interface{} `json:"maxSupply"`
	Provider             string `json:"provider"`
	LastUpdatedTimestamp string `json:"lastUpdatedTimestamp"`
}

func handler(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	symbol := params["symbol"]
	tmp := strings.ToUpper(symbol)
	symbolPro := strings.TrimSpace(tmp)
	fmt.Printf("You are querying %s info",symbol)
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error in reading body: %v",err)
		http.Error(w, "can't read body", http.StatusBadRequest)
	}
	var requestBody RequestBody
	err = json.Unmarshal(body, &requestBody)
	var currencyCode []string
	if err != nil {
		currencyCode = currencyCodeDefault
	} else {
		currencyCode = requestBody.CurrencyCode
	}
	ctx := context.TODO()
	rds := initializeRedisLocalClient(ctx)
	res, err := rds.Get(ctx,symbolPro).Result()
	if err != nil {
		fmt.Println("No redis data and querying API now!")
		currencyPrice,coinBaseErr,errCode:= getCoinBaseInfo(w,symbolPro,currencyCode)

		if errCode == 404 {
			return
		}

		tokenInfo,coinMarketErr := getCoinMarketInfo(w,symbolPro)

		tokenInfoMap,coinGeckoErr := getCoinGeckoInfo(w,symbolPro,currencyCode)

		workMode := checkAPI(coinBaseErr,coinMarketErr,coinGeckoErr)

		switch workMode {
		case 0:
			processBMG(w,symbolPro,currencyPrice,tokenInfo,tokenInfoMap)
			fmt.Println("========processBMG========")

		case 1:
			processMG(w,symbolPro,currencyPrice,tokenInfo,tokenInfoMap)
			fmt.Println("========processMG==========")
		case 2:
			processBG(w,symbolPro,currencyPrice,tokenInfo,tokenInfoMap)
			fmt.Println("========processBG=========")
		case 3:
			processBM(w,symbolPro,currencyPrice,tokenInfo,tokenInfoMap)
			fmt.Println("========processBM=========")
		case 4:
			processG(w,symbolPro,currencyPrice,tokenInfo,tokenInfoMap)
			fmt.Println("========processG=========")
		case -1:
			msg,_ := json.Marshal(errResult{400,"Api server error"})
			w.Write(msg)

		}
	} else {
		var redisJson Redis
		err := json.Unmarshal([]byte(res),&redisJson)
		if err != nil {
			fmt.Println("Error decoding redis data")
		}
		currencyPrice,_,errCode:= getCoinBaseInfo(w,symbolPro,currencyCode)
		if errCode == 404 {
			return
		}
		processRedis(w,symbolPro,currencyPrice,redisJson)

	}







}

func getCoinMarketInfo(w http.ResponseWriter, symbol string) (map[string]interface{}, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET",coinMarketApi, nil)
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}
	q := url.Values{}
	q.Add("symbol",symbol)
	q.Add("convert","USD")
	req.Header.Set("Accepts", "application/json")
	req.Header.Add("X-CMC_PRO_API_KEY", "2be37802-e3cc-4a4d-8418-f1a39ce0f613")
	req.URL.RawQuery = q.Encode()
	resp, err := client.Do(req)
	if err != nil {
		msg, _ := json.Marshal(errResult{400, "Error getting cryptocurrency Supply"})
		fmt.Println("Error getting cryptocurrency totalSupply")
		w.Write(msg)
		return nil, err
	}
	respBody, _ := ioutil.ReadAll(resp.Body)
	var tokenInfo = make(map[string]interface{})
	checkStatus := gjson.Get(string(respBody), "status.error_code").Int()
	//fmt.Println(checkStatus != 0)
	if checkStatus != 0 {
		msg, _ := json.Marshal(errResult{400, "CoinMarket API limited"})
		fmt.Println("CoinMarket API limited")
		w.Write(msg)
		return nil, errors.New("API limited")
	}
	var currency = "data."+symbol
	checkCorrectCurrency := gjson.Get(string(respBody),currency).Exists()
	if !checkCorrectCurrency {
		fmt.Println("CoinMarket data error")
		return nil, errors.New("CoinMarket data error")
	}


	var slug = "data." + symbol + ".slug"
	var maxSupply = "data." + symbol + ".max_supply"
	var circulatingSupply = "data." + symbol + ".circulating_supply"
	var lastUpdated = "data." + symbol + ".last_updated"
	//fmt.Println(string(respBody))
	checkMaxSupply :=  gjson.Get(string(respBody),maxSupply).Type.String()=="Null"
	if !checkMaxSupply {
		tokenInfo["maxSupply"] = gjson.Get(string(respBody),maxSupply).Float()
	}
	tokenInfo["provider"] = gjson.Get(string(respBody), slug).String()
	tokenInfo["circulatingSupply"] = gjson.Get(string(respBody),circulatingSupply).Float()
	tokenInfo["lastUpdatedTimestamp"] = gjson.Get(string(respBody),lastUpdated).String()

	//w.Write(respBody)
	fmt.Println("==========CoinMarket===========")
	fmt.Println(tokenInfo)
	fmt.Println("===============================")
	return tokenInfo,nil
}

func getCoinBaseInfo(w http.ResponseWriter,symbol string, currencyCode []string) (map[string]float64 ,error, int){
	client := &http.Client{}
	req, err := http.NewRequest("GET",coinBaseApi,nil)
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}
	q := url.Values{}
	q.Add("currency",symbol)
	req.Header.Set("Accepts", "application/json")
	req.URL.RawQuery = q.Encode()
	resp, err := client.Do(req)
	if err != nil {
		msg, _ := json.Marshal(errResult{400, "Error getting cryptocurrency prices"})
		fmt.Println("Error getting cryptocurrency prices")
		w.Write(msg)
		return nil, errors.New("Error getting cryptocurrency prices"),400

	}
	//fmt.Println(resp.Status)
	respBody, _ := ioutil.ReadAll(resp.Body)
	var currencyPrice = make(map[string]float64)
	checkStatus := gjson.Get(string(respBody),"errors").Exists()
	if checkStatus {
		msg, _ := json.Marshal(errResult{404, "cryptocurrency "+ symbol+" doesn't exist"})
		w.Write(msg)
		return nil, errors.New("Error getting CoinBase price"),404
	}
	for _, code := range currencyCode {
		tmp := strings.ToUpper(code)
		 code:= strings.TrimSpace(tmp)

		var path  = "data.rates."+code
		if !gjson.Get(string(respBody), path).Exists() {
			msg, _ := json.Marshal(errResult{404, "Error cryptocurrency code "+code})
			w.Write(msg)
			return nil, errors.New("Error cryptocurrency code"+code),404
		}  else {
			currencyPrice[code]= gjson.Get(string(respBody), path).Float()
			//fmt.Println(gjson.Get(string(respBody), path).Float())
			}
	}
	fmt.Println("==========CurrencyPrice===========")
	fmt.Println(currencyPrice)
	fmt.Println("==================================")
	return currencyPrice,nil,0
}

func getCoinGeckoInfo( w http.ResponseWriter,symbol string, currencyCode []string)(map[string]map[string]interface{},error){
	var tokenInfoMap = make(map[string]map[string]interface{})
	for _, code := range currencyCode {
		tmp := strings.ToUpper(code)
		code:= strings.TrimSpace(tmp)
		client := &http.Client{}
		req, err := http.NewRequest("GET",coinGeckoApi, nil)
		if err != nil {
			log.Print(err)
			os.Exit(1)
		}
		ctx := context.TODO()
		co := initializeMongoLocalClient(ctx)
		id := getSymbolId(co,symbol)
		fmt.Println("==========TokenId==========")
		fmt.Println(id)
		fmt.Println("===============================")

		q := url.Values{}
		q.Add("vs_currency",code)
		q.Add("ids",id)
		req.Header.Set("Accepts","application/json")
		req.URL.RawQuery = q.Encode()
		resp, err := client.Do(req)
		if err != nil {
			msg, _ := json.Marshal(errResult{400, "Error getting cryptocurrency info"})
			fmt.Println("Error getting cryptocurrency prices")
			w.Write(msg)
			return nil, errors.New("Error getting cryptocurrency prices")
		}
		respBody, _ := ioutil.ReadAll(resp.Body)
		var tokenInfo = make(map[string]interface{})
		//fmt.Println(string(respBody))
		if gjson.Get(string(respBody),"error").Exists() {
			msg, _ := json.Marshal(errResult{400, "Invalid currency "+ code})
			fmt.Println("Invalid currency "+code)
			w.Write(msg)
			return nil, errors.New("Invalid currency"+code)
		}
		var coinGeckoMarket = make([]CoinGeckoMarket,0)
		err = json.Unmarshal([]byte(string(respBody)), &coinGeckoMarket)
		//fmt.Println(coinGeckoMarket)
		if err != nil {
			fmt.Println("Error decoding coinGeckoMarket Info")
			return nil ,err

		}
		//fmt.Println(coinGeckoMarket[0].Image)
		
		tokenInfo["price"] = float64(coinGeckoMarket[0].CurrentPrice)
		checkMaxSupply := coinGeckoMarket[0].MaxSupply == nil
		if !checkMaxSupply {
			tokenInfo["maxSupply"] = coinGeckoMarket[0].MaxSupply.(float64)
		}
		tokenInfo["circulatingSupply"] = float64(coinGeckoMarket[0].CirculatingSupply)
		tokenInfo["marketCap"] = float64(coinGeckoMarket[0].MarketCap)
		tokenInfo["lastUpdatedTimestamp"] = coinGeckoMarket[0].LastUpdated.String()

		tokenInfoMap[code] = tokenInfo
	}
	fmt.Println("===========CoinGecko============")
	fmt.Println(tokenInfoMap)
	fmt.Println("================================")
	return tokenInfoMap,nil

}

func checkAPI(coinBaseErr error,coinMarketErr error, coinGeckoErr error) int {
	//三个API同时工作
	if coinBaseErr == nil && coinMarketErr == nil && coinGeckoErr == nil {
		return 0
		//coinBase不工作
	} else if coinBaseErr != nil && coinMarketErr == nil && coinGeckoErr == nil {
		return 1
		//coinMarket不工作
	} else if coinBaseErr == nil && coinMarketErr != nil && coinGeckoErr == nil {
		return 2
		//coinBase不工作
	} else if coinBaseErr == nil && coinMarketErr == nil && coinGeckoErr != nil {
		return 3
		//coinBase和coinMarket同时不工作
	} else if coinBaseErr != nil && coinMarketErr != nil && coinGeckoErr == nil {
		return 4
	} else {
		return -1
	}

}
func processBMG(w http.ResponseWriter,symbolPro string,currencyPrice map[string]float64, tokenInfo map[string]interface{}, tokenInfoMap map[string]map[string]interface{}){
	var data Data
	var res []Data
	for k,v := range currencyPrice {
		data.Symbol = symbolPro
		data.CurrencyCode = k
		data.Price = v
		data.CirculatingSupply = tokenInfoMap[k]["circulatingSupply"].(float64)
		//data.CirculatingSupply = tokenInfo["circulatingSupply"].(int64)
		data.MarketCap = float64(data.CirculatingSupply) *data.Price
		data.AccTradePrice24H = nil
		if reflect.TypeOf(tokenInfo["maxSupply"])!=nil {
			data.MaxSupply = tokenInfo["maxSupply"].(float64)
		} else {
			data.MaxSupply = nil
		}

		data.Provider = tokenInfo["provider"].(string)
		//fmt.Println(reflect.TypeOf(tokenInfoMap[k]["lastUpdatedTimestamp"]))
		data.LastUpdatedTimestamp = tokenInfoMap[k]["lastUpdatedTimestamp"].(string)
		res = append(res,data)

	}
	ctx := context.TODO()
	rds := initializeRedisLocalClient(ctx)
	redis := Redis{data.MarketCap,data.CirculatingSupply,data.MaxSupply,data.Provider,data.LastUpdatedTimestamp}
	redisJson, err := json.Marshal(redis)
	if err != nil {
		fmt.Println("Encoding error")
	}
	err = rds.Set(ctx,symbolPro,redisJson,300*time.Second).Err()
	if err != nil {
		fmt.Println("Set redis error")
	}
	result, err := json.Marshal(res)
	if err != nil {
		fmt.Println(err)
	}
	w.Write(result)
}
func processRedis(w http.ResponseWriter,symbolPro string,currencyPrice map[string]float64, redisJson Redis){
	var data Data
	var res []Data
	for k,v := range currencyPrice {
		data.Symbol = symbolPro
		data.CurrencyCode = k
		data.Price = v
		data.CirculatingSupply = redisJson.CirculatingSupply
		//data.CirculatingSupply = tokenInfo["circulatingSupply"].(int64)
		data.MarketCap = redisJson.MarketCap
		data.AccTradePrice24H = nil
		data.MaxSupply = redisJson.MaxSupply
		data.Provider = redisJson.Provider
		//fmt.Println(reflect.TypeOf(tokenInfoMap[k]["lastUpdatedTimestamp"]))
		data.LastUpdatedTimestamp = redisJson.LastUpdatedTimestamp
		res = append(res,data)

	}
	result, err := json.Marshal(res)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("=============Get redis data============")
	w.Write(result)
}
func processMG(w http.ResponseWriter,symbolPro string,currencyPrice map[string]float64, tokenInfo map[string]interface{}, tokenInfoMap map[string]map[string]interface{}){
	var data Data
	var res []Data
	for k,_ := range currencyPrice {
		data.Symbol = symbolPro
		data.CurrencyCode = k
		data.Price = tokenInfoMap[k]["price"].(float64)
		data.CirculatingSupply = tokenInfoMap[k]["circulatingSupply"].(float64)
		//data.CirculatingSupply = tokenInfo["circulatingSupply"].(int64)
		data.MarketCap = tokenInfoMap[k]["marketCap"].(float64)
		data.AccTradePrice24H = nil
		if reflect.TypeOf(tokenInfo["maxSupply"])!=nil {
			data.MaxSupply = tokenInfo["maxSupply"].(float64)
		} else {
			data.MaxSupply = nil
		}
		data.Provider = tokenInfo["provider"].(string)
		data.LastUpdatedTimestamp = tokenInfoMap[k]["lastUpdatedTimestamp"].(string)
		res = append(res,data)

	}
	ctx := context.TODO()
	rds := initializeRedisLocalClient(ctx)
	redis := Redis{data.MarketCap,data.CirculatingSupply,data.MaxSupply,data.Provider,data.LastUpdatedTimestamp}
	redisJson, err := json.Marshal(redis)
	if err != nil {
		fmt.Println("Encoding error")
	}
	err = rds.Set(ctx,symbolPro,redisJson,300*time.Second).Err()
	if err != nil {
		fmt.Println("Set redis error")
	}

	result, err := json.Marshal(res)
	if err != nil {
		fmt.Println(err)
	}
	w.Write(result)
}
func processBG(w http.ResponseWriter,symbolPro string,currencyPrice map[string]float64, tokenInfo map[string]interface{}, tokenInfoMap map[string]map[string]interface{}){
	var data Data
	var res []Data
	for k,v := range currencyPrice {
		data.Symbol = symbolPro
		data.CurrencyCode = k
		data.Price = v
		data.CirculatingSupply = tokenInfoMap[k]["circulatingSupply"].(float64)
		//data.CirculatingSupply = tokenInfo["circulatingSupply"].(int64)
		data.MarketCap = float64(data.CirculatingSupply) *data.Price
		data.AccTradePrice24H = nil
		if reflect.TypeOf(tokenInfoMap[k]["maxSupply"])!=nil {
			data.MaxSupply = tokenInfoMap[k]["maxSupply"].(float64)
		} else {
			data.MaxSupply = nil
		}
		data.Provider = "error"
		data.LastUpdatedTimestamp = tokenInfoMap[k]["lastUpdatedTimestamp"].(string)
		res = append(res,data)

	}
	ctx := context.TODO()
	rds := initializeRedisLocalClient(ctx)
	redis := Redis{data.MarketCap,data.CirculatingSupply,data.MaxSupply,data.Provider,data.LastUpdatedTimestamp}
	redisJson, err := json.Marshal(redis)
	if err != nil {
		fmt.Println("Encoding error")
	}
	err = rds.Set(ctx,symbolPro,redisJson,300*time.Second).Err()
	if err != nil {
		fmt.Println("Set redis error")
	}
	result, err := json.Marshal(res)
	if err != nil {
		fmt.Println(err)
	}
	w.Write(result)
}

func processBM(w http.ResponseWriter,symbolPro string,currencyPrice map[string]float64, tokenInfo map[string]interface{}, tokenInfoMap map[string]map[string]interface{}){
	var data Data
	var res []Data
	for k,v := range currencyPrice {
		data.Symbol = symbolPro
		data.CurrencyCode = k
		data.Price = v
		data.CirculatingSupply = tokenInfo["circulatingSupply"].(float64)
		//data.CirculatingSupply = tokenInfo["circulatingSupply"].(int64)
		data.MarketCap = float64(data.CirculatingSupply) *data.Price
		data.AccTradePrice24H = nil

		if reflect.TypeOf(tokenInfo["maxSupply"])!=nil {
			data.MaxSupply = tokenInfo["maxSupply"].(float64)
		} else {
			data.MaxSupply = nil
		}
		data.Provider = tokenInfo["provider"].(string)
		data.LastUpdatedTimestamp = tokenInfo["lastUpdatedTimestamp"].(string)
		res = append(res,data)

	}
	ctx := context.TODO()
	rds := initializeRedisLocalClient(ctx)
	redis := Redis{data.MarketCap,data.CirculatingSupply,data.MaxSupply,data.Provider,data.LastUpdatedTimestamp}
	redisJson, err := json.Marshal(redis)
	if err != nil {
		fmt.Println("Encoding error")
	}
	err = rds.Set(ctx,symbolPro,redisJson,300*time.Second).Err()
	if err != nil {
		fmt.Println("Set redis error")
	}
	result, err := json.Marshal(res)
	if err != nil {
		fmt.Println(err)
	}
	w.Write(result)
}
func processG(w http.ResponseWriter,symbolPro string,currencyPrice map[string]float64, tokenInfo map[string]interface{}, tokenInfoMap map[string]map[string]interface{}){
	var data Data
	var res []Data
	for k,_ := range currencyPrice {
		data.Symbol = symbolPro
		data.CurrencyCode = k
		data.Price = tokenInfoMap[k]["price"].(float64)
		data.CirculatingSupply = tokenInfoMap[k]["circulatingSupply"].(float64)
		//data.CirculatingSupply = tokenInfo["circulatingSupply"].(int64)
		data.MarketCap = float64(data.CirculatingSupply) *data.Price
		data.AccTradePrice24H = nil
		if reflect.TypeOf(tokenInfoMap[k]["maxSupply"])!=nil {
			data.MaxSupply = tokenInfoMap[k]["maxSupply"].(float64)
		} else {
			data.MaxSupply = nil
		}
		data.Provider = "error"
		data.LastUpdatedTimestamp = tokenInfoMap[k]["lastUpdatedTimestamp"].(string)
		res = append(res,data)

	}
	ctx := context.TODO()
	rds := initializeRedisLocalClient(ctx)
	redis := Redis{data.MarketCap,data.CirculatingSupply,data.MaxSupply,data.Provider,data.LastUpdatedTimestamp}
	redisJson, err := json.Marshal(redis)
	if err != nil {
		fmt.Println("Encoding error")
	}
	err = rds.Set(ctx,symbolPro,redisJson,300*time.Second).Err()
	if err != nil {
		fmt.Println("Set redis error")
	}
	result, err := json.Marshal(res)
	if err != nil {
		fmt.Println(err)
	}
	w.Write(result)
}

func initializeRedisLocalClient( ctx context.Context) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1:6381",
		Password: "", // no password set
		DB:       0,  // use default DB
	})
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		fmt.Println("redis链接错误")
	}
	return rdb
}
func initializeMongoLocalClient( ctx context.Context) *mongo.Client {
	var clientOptions *options.ClientOptions
	clientOptions = options.Client().ApplyURI("mongodb://127.0.0.1:27006/id")
	cl, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		fmt.Println("connect mongo error")
	}
	err = cl.Ping(ctx, nil)
	if err != nil {
		fmt.Println("ping mongo error")
	}
	return cl
}
func setSymbolId() {
	ctx := context.TODO()
	co := initializeMongoLocalClient(ctx)
	client := &http.Client{}
	req, err := http.NewRequest("GET",symbolIdApi, nil)
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}
	req.Header.Set("Accepts", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error getting symbolId")
	}
	respBody, _ := ioutil.ReadAll(resp.Body)
	var symbolIdList []SymbolId
	//fmt.Println(string(respBody))
	err = json.Unmarshal([]byte(string(respBody)), &symbolIdList)
	if err != nil {
		fmt.Println("Error decoding symbolId Info")
	}
	//fmt.Println(symbolIdList)
	co.Database("id").Collection("symbolId").Drop(ctx)
	for _,symbolId := range symbolIdList {
		insertOne, err := co.Database("id").Collection("symbolId").InsertOne(ctx,symbolId)
		if err != nil {
			fmt.Println("Insert Error")
		}
		fmt.Println(insertOne)
		}
}
func getSymbolId (co *mongo.Client,symbol string) string {
	ctx := context.TODO()
	tmp := strings.ToLower(symbol)
	symbolP := strings.TrimSpace(tmp)
	filter := bson.M{"symbol": symbolP}
	fmt.Println("==========TokenSymbol==========")
	fmt.Println(symbolP)
	fmt.Println("===============================")
	result := co.Database("id").Collection("symbolId").FindOne(ctx,filter )
	var symbolId SymbolId
	err :=result.Decode(&symbolId)
	if err != nil {
		fmt.Println("Decode symbolId error")
	}
	return symbolId.Id

}