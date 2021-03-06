package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/thanhpk/randstr"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	qrcode "github.com/skip2/go-qrcode"
)

type ShrlType int

const (
	ShortenedUrl ShrlType = iota
	UploadedFile
	TextSnippet
)

func (t ShrlType) String() string {
	return []string{"ShortUrl", "UploadedFile", "TextSnippet"}[t]
}

type URL struct {
	ID             primitive.ObjectID `bson:"_id" json:"id"`
	Alias          string             `bson:"alias" json:"alias"`
	Location       string             `bson:"location" json:"location"`
	UploadLocation string             `bson:"upload_location" json:"-"`
	SnippetTitle   string             `bson:"snippet_title" json:"snippet_title,omitempty"`
	Snippet        string             `bson:"snippet" json:"snippet,omitempty"`
	CreatedAt      time.Time          `bson:"created_at" json:"created_at"`
	Views          int                `bson:"views" json:"views"`
	Tags           []string           `bson:"tags" json:"tags"`
	Type           ShrlType           `bson:"type" json:"type"`
}

func (u URL) Delete() error {
	switch u.Type {
	case UploadedFile:
		os.Remove(u.UploadLocation)
	}

	_, err := collection.DeleteOne(ctx, bson.M{"_id": u.ID})
	return err
}

func (u URL) Update() error {
	_, err := collection.UpdateByID(ctx, u.ID, bson.D{
		{"$set", u},
	})
	return err
}

func (u *URL) Create() error {
	u.Cleanse()
	_, err := collection.InsertOne(ctx, u)
	return err
}

func (u URL) IncrementViews() error {
	_, err := collection.UpdateByID(ctx, u.ID, bson.D{
		{"$inc", bson.D{{"views", 1}}},
	})
	return err

}

func (u URL) ToQR(w io.Writer) error {
	location := u.Location
	if u.Type != ShortenedUrl {
		location = Settings.BaseURL + "/" + u.Alias
	}
	result, err := qrcode.New(location, qrcode.Medium)
	if err != nil {
		return err
	}
	result.Write(256, w)
	return nil
}

func (u URL) toTextQR(w io.Writer) error {
	location := u.Location
	if u.Type != ShortenedUrl {
		location = Settings.BaseURL + "/" + u.Alias
	}
	result, err := qrcode.New(location, qrcode.Medium)
	if err != nil {
		return err
	}
	w.Write([]byte(result.ToSmallString(false)))
	w.Write([]byte(fmt.Sprintf("\nScan the code above or visit %s in a browser.\n", location)))
	return nil
}

func (u URL) ToText(w io.Writer) {
	switch u.Type {
	case ShortenedUrl:
		w.Write([]byte(u.Location))
	case TextSnippet:
		w.Write([]byte(u.Snippet))
	case UploadedFile:
		w.Write([]byte("Unable to write binary to text"))
	}
}

func (u URL) FriendlyAlias() string {
	var strs []string
	strs = append(strs, Settings.BaseURL)
	strs = append(strs, u.Alias)
	return strings.Join(strs, "/")
}

func (u *URL) ResolveLocation() error {
	nextUrl := u.Location

	var i int
	for i < 25 {
		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}

		resp, err := client.Head(nextUrl)
		if err != nil {
			return err
		}

		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			nextUrl = resp.Header.Get("Location")
			i += 1
		} else {
			break
		}
	}

	u.Location = nextUrl
	return nil
}

func (u *URL) StripParams() error {
	location, err := url.Parse(u.Location)
	if err != nil {
		return err
	}
	new_location := url.URL{
		Scheme: location.Scheme,
		User:   location.User,
		Host:   location.Host,
		Path:   location.Path,
	}

	u.Location = new_location.String()
	return nil
}

func (u *URL) Cleanse() {
	if u.Type != ShortenedUrl {
		return
	}

	ur, err := url.Parse(u.Location)
	if err != nil {
		return
	}

	resolveLocation := false
	for _, v := range Settings.ResolveLocationHosts {
		resolveLocation = strings.ToLower(ur.Host) == strings.ToLower(v)
		if resolveLocation {
			break
		}
	}

	stripParams := false
	for _, v := range Settings.StripQueryParamsHosts {
		stripParams = strings.ToLower(ur.Host) == strings.ToLower(v)
		if stripParams {
			break
		}
	}

	if resolveLocation {
		u.ResolveLocation()
	}

	if stripParams {
		u.StripParams()
	}
}

func (u *URL) Redirect(w http.ResponseWriter, r *http.Request) {
	switch u.Type {
	case ShortenedUrl:
		http.Redirect(w, r, u.Location, http.StatusPermanentRedirect)

	case UploadedFile:
		writeFile(u, w)

	case TextSnippet:
		w.Write([]byte(u.Snippet))
	}
}

type URLs struct {
	Urls []*URL `json:"shrls"`
}

func NewAlias() string {
	alias := randstr.String(5)
	for aliasExists(alias) {
		alias = randstr.String(5)
	}
	return alias
}

func aliasExists(alias string) bool {
	filter := bson.D{
		primitive.E{Key: "alias", Value: alias},
	}
	urls, err := filterUrls(filter)
	if err != nil {
		return false
	}
	return len(urls) == 0
}

func NewURL() URL {
	url := URL{
		ID:        primitive.NewObjectID(),
		CreatedAt: time.Now(),
		Alias:     NewAlias(),
		Type:      ShortenedUrl,
	}
	return url
}

func urlByID(url_id string) (*URL, error) {
	var url *URL
	_id, err := primitive.ObjectIDFromHex(url_id)
	if err != nil {
		return url, err
	}

	cur := collection.FindOne(ctx, bson.M{"_id": &_id})
	err = cur.Decode(&url)
	return url, err
}

func updateUrl(url *URL) error {
	_, err := collection.UpdateByID(ctx, url.ID, bson.D{
		{"$set", url},
	})
	return err
}

func deleteUrl(url_id string) error {
	id, err := primitive.ObjectIDFromHex(url_id)
	if err != nil {
		return err
	}
	_, err = collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func ShrlFromString(url string) URL {
	shrl := NewURL()
	shrl.Location = url
	shrl.Create()
	return shrl
}

type PaginationParameters struct {
	Search string `json:"search"`
	Skip   int64  `json:"skip"`
	Limit  int64  `json:"limit"`
}

func paginatedUrls(prm PaginationParameters) ([]*URL, int64, error) {
	var urls []*URL

	regex := fmt.Sprintf(".*%s.*", prm.Search)

	filter := bson.D{{
		"$or",
		bson.A{
			bson.D{{
				"alias",
				bson.D{{
					"$regex",
					primitive.Regex{Pattern: regex, Options: "i"},
				}},
			}},
			bson.D{{
				"location",
				bson.D{{
					"$regex",
					primitive.Regex{Pattern: regex, Options: "i"},
				}},
			}},
			bson.D{{
				"tags",
				bson.D{{
					"$regex",
					primitive.Regex{Pattern: regex, Options: "i"},
				}},
			}},
		},
	}}

	count, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		count = -1
	}

	opts := options.FindOptions{
		Skip:  &prm.Skip,
		Limit: &prm.Limit,
	}

	opts.SetSort(bson.D{{"created_at", -1}})

	cur, err := collection.Find(ctx, filter, &opts)
	if err != nil {
		return urls, count, err
	}

	for cur.Next(ctx) {
		var u URL
		err := cur.Decode(&u)
		if err != nil {
			return urls, count, err
		}
		urls = append(urls, &u)
	}
	if err := cur.Err(); err != nil {
		return urls, count, err
	}
	return urls, count, nil
}

func filterUrls(filter interface{}) ([]*URL, error) {
	var urls []*URL

	cur, err := collection.Find(ctx, filter)
	if err != nil {
		return urls, err
	}

	for cur.Next(ctx) {
		var u URL
		err := cur.Decode(&u)
		if err != nil {
			return urls, err
		}

		urls = append(urls, &u)
	}

	if err := cur.Err(); err != nil {
		return urls, err
	}

	cur.Close(ctx)

	if len(urls) == 0 {
		return urls, mongo.ErrNoDocuments
	}

	return urls, nil
}
