package controller

import (
	"encoding/json"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"golang.org/x/crypto/bcrypt"
	"log"
	"strings"
	"time"
	"url_shortner/constants"
	"url_shortner/dto/shortUrl"
	"url_shortner/models"
	"url_shortner/services"
	"url_shortner/services/jwtService"
	"url_shortner/util"
)

type ShortURLController struct {
	ShortUrlMapService services.ShortUrlsService
	UserService        services.UserService
	Cache              *util.LRUCache
}

func (controller *ShortURLController) Create(ctx *fiber.Ctx) error {
	createShortUrl := shortUrl.CreateShortUrlTO{}
	err := ctx.BodyParser(&createShortUrl)

	if err != nil {
		log.Println(err.Error())
		return util.GenerateResponse(ctx, "Something Went Wrong", false, "Something Went Wrong")
	}

	cookie := ctx.Cookies(constants.CookieKey)

	userId, errGetId := jwtService.GetUserId(cookie)

	if errGetId != nil {
		log.Println(errGetId.Error())
		return util.GenerateResponse(ctx, "Unauthorized", false, "Not Logged in ")

	}

	userObjectId, errGetUserObjectId := primitive.ObjectIDFromHex(userId)

	if errGetUserObjectId != nil {
		log.Println(errGetUserObjectId.Error())
		return util.GenerateResponse(ctx, "Something Went Wrong", false, "Something Went Wrong")
	}

	_, errGetUser := controller.UserService.GenericMongo.FindOne(util.GetFieldBsonTag[models.User]([]models.User{{ID: userObjectId}}), []any{userObjectId})

	if errGetUser != nil {
		return util.GenerateResponse(ctx, "User not Found", false, "Something Went Wrong")
	}

	urlId := uuid.New().String()[0:5]

	errCreate := controller.ShortUrlMapService.Create(urlId, createShortUrl.LongURL, createShortUrl.Passworded, createShortUrl.Password, int(time.Now().Add(time.Hour*24*30).UnixMilli()), userId)

	if errCreate != nil {
		log.Println(errCreate.Error())
		return util.GenerateResponse(ctx, "Something Went Wrong", false, errCreate.Error())
	}

	return util.GenerateResponse(ctx, urlId, true, "Generated Short URL successfully")

}

func (controller *ShortURLController) RedirectShortUrl(ctx *fiber.Ctx) error {

	shortUrlId := ctx.Params("id")

	passwordString := ctx.Query("password")

	if strings.TrimSpace(shortUrlId) == "" {
		return util.GenerateResponse(ctx, "", false, "No ID provided")
	}

	var shortUrlData models.ShortURLMap

	cacheData := controller.Cache.Get(shortUrlId)

	if cacheData == nil {
		log.Println("Cache Miss")
		data, errShortUrl := controller.ShortUrlMapService.GenericMongo.FindOne(util.GetFieldBsonTag[models.ShortURLMap]([]models.ShortURLMap{{UrlId: shortUrlId}}), []any{shortUrlId})

		if errShortUrl != nil {
			log.Println(errShortUrl.Error())
			return util.GenerateResponse(ctx, "", false, "An Error Occurred")
		}

		shortUrlData = data

		controller.Cache.Put(shortUrlId, data)
	} else {
		log.Println("Cache Hit")
		errConvert := json.Unmarshal(cacheData.Data, &shortUrlData)
		if errConvert != nil {
			log.Println(errConvert.Error())
		}
	}

	if int(time.Now().UnixMilli()) > shortUrlData.ExpiryDate {
		return util.GenerateResponse(ctx, "", false, "Expired Short URL")
	}

	if shortUrlData.Passworded == false {
		return ctx.Redirect(shortUrlData.LongURL, 301)
	}

	if shortUrlData.Passworded == true && (strings.TrimSpace(passwordString) == "") {
		return util.GenerateResponse(ctx, "", false, "Password not Provided ")
	}

	errComparePass := bcrypt.CompareHashAndPassword([]byte(shortUrlData.Password), []byte(passwordString))

	if errComparePass != nil {
		log.Println(errComparePass.Error())
		return util.GenerateResponse(ctx, "", false, "Password Provided, is Incorrect ")
	}

	go func() {

		keys := util.GetFieldBsonTag[models.ShortURLMap]([]models.ShortURLMap{{UrlId: shortUrlId}})

		updated, errUpdate := controller.ShortUrlMapService.GenericMongo.Update(keys, []any{shortUrlId}, util.GetFieldBsonTag[models.ShortURLMap]([]models.ShortURLMap{{NumberOfHits: 1}}), []any{shortUrlData.NumberOfHits + 1})

		if errUpdate != nil {
			log.Println(errUpdate.Error())
		}

		shortUrlData.NumberOfHits += 1

		controller.Cache.Put(shortUrlId, shortUrlData)

		log.Println(updated, "Updated Short URL")
	}()

	return ctx.Redirect(shortUrlData.LongURL)

}
