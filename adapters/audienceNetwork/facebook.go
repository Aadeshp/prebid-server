package audienceNetwork

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/mxmCherry/openrtb"
	"github.com/prebid/prebid-server/adapters"
	"github.com/prebid/prebid-server/errortypes"
	"github.com/prebid/prebid-server/openrtb_ext"
)

type FacebookAdapter struct {
	http         *adapters.HTTPAdapter
	URI          string
	nonSecureUri string
	platformId   string
}

// used for cookies and such
func (a *FacebookAdapter) Name() string {
	return "audienceNetwork"
}

func (a *FacebookAdapter) SkipNoCookies() bool {
	return false
}

type facebookReqExt struct {
	PlatformId string `json:"platformid"`
}

func (this *FacebookAdapter) MakeRequests(request *openrtb.BidRequest, reqInfo *adapters.ExtraRequestInfo) ([]*adapters.RequestData, []error) {
	var errs []error

	if len(request.Imp) == 0 {
		errs = append(errs, &errortypes.BadInput{
			Message: "no impressions provided",
		})
		return nil, errs
	}

	// Documentation suggests bid request splitting by impression so that each
	// request only represents a single impression
	reqs := make([]*adapters.RequestData, 0, len(request.Imp))
	headers := http.Header{}

	headers.Add("Content-Type", "application/json;charset=utf-8")
	headers.Add("Accept", "application/json")

	for _, imp := range request.Imp {
		// Make a copy of the request so that we don't change the original request which
		// is shared across multiple threads
		fbreq := *request

		plmtId, pubId, err := this.extractPlmtAndPub(&imp)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		reqExt := facebookReqExt{PlatformId: this.platformId}
		if fbreq.Ext, err = json.Marshal(reqExt); err != nil {
			errs = append(errs, err)
			continue
		}

		imp.TagID = fmt.Sprintf("%s_%s", pubId, plmtId)
		imp.Ext = nil
		fbreq.Imp = []openrtb.Imp{imp}

		if fbreq.App != nil {
			app := *fbreq.App
			app.Publisher = &openrtb.Publisher{ID: pubId}
			fbreq.App = &app
		} else {
			site := *fbreq.Site
			site.Publisher = &openrtb.Publisher{ID: pubId}
			fbreq.Site = &site
		}

		body, err := json.Marshal(&fbreq)
		if err != nil {
			errs = append(errs, err)
			return nil, errs
		}

		reqs = append(reqs, &adapters.RequestData{
			Method:  "POST",
			Uri:     this.URI,
			Body:    body,
			Headers: headers,
		})
	}

	if len(reqs) == 0 {
		errs = append(errs, &errortypes.BadInput{
			Message: "no valid impressions provided",
		})
		return nil, errs
	}

	return reqs, errs
}

func (this *FacebookAdapter) extractPlmtAndPub(out *openrtb.Imp) (string, string, error) {
	var bidderExt adapters.ExtImpBidder
	if err := json.Unmarshal(out.Ext, &bidderExt); err != nil {
		return "", "", &errortypes.BadInput{
			Message: err.Error(),
		}
	}

	var fbExt openrtb_ext.ExtImpFacebook
	if err := json.Unmarshal(bidderExt.Bidder, &fbExt); err != nil {
		return "", "", &errortypes.BadInput{
			Message: err.Error(),
		}
	}

	if fbExt.PlacementId == "" {
		return "", "", &errortypes.BadInput{
			Message: "Missing placementId param",
		}
	}

	placementId := fbExt.PlacementId
	publisherId := fbExt.PublisherId

	// Support the legacy path with the caller was expected to pass in just placementId
	// which was an underscore concantenated string with the publisherId and placementId.
	// The new path for callers is to pass in the placementId and publisherId independently
	// and the below code will prefix the placementId that we pass to FAN with the publsiherId
	// so that we can abstract the implementation details from the caller
	toks := strings.Split(placementId, "_")
	if len(toks) == 1 {
		if publisherId == "" {
			return "", "", &errortypes.BadInput{
				Message: "Missing publisherId param",
			}
		}

		return placementId, publisherId, nil
	} else if len(toks) == 2 {
		publisherId = toks[0]
		placementId = toks[1]
	} else {
		return "", "", &errortypes.BadInput{
			Message: fmt.Sprintf("Invalid placementId param '%s' and publisherId param '%s'", placementId, publisherId),
		}
	}

	return placementId, publisherId, nil
}

func (this *FacebookAdapter) MakeBids(request *openrtb.BidRequest, adapterRequest *adapters.RequestData, response *adapters.ResponseData) (*adapters.BidderResponse, []error) {
	if response.StatusCode != http.StatusOK {
		msg := response.Headers.Get("x-fb-an-errors")
		return nil, []error{&errortypes.BadInput{
			Message: fmt.Sprintf("Unexpected status code %d with error message '%s'", response.StatusCode, msg),
		}}
	}

	var bidResp openrtb.BidResponse
	if err := json.Unmarshal(response.Body, &bidResp); err != nil {
		return nil, []error{err}
	}

	out := adapters.NewBidderResponseWithBidsCapacity(4)
	for _, seatbid := range bidResp.SeatBid {
		for _, bid := range seatbid.Bid {
			out.Bids = append(out.Bids, &adapters.TypedBid{
				Bid:     &bid,
				BidType: openrtb_ext.BidTypeBanner,
			})
		}
	}

	return out, nil
}

func NewFacebookBidder(client *http.Client, platformId string) *FacebookAdapter {
	/*if platformId == "" {
		glog.Errorf("No facebook partnerID specified. Calls to the Audience Network will fail. Did you set adapters.facebook.platform_id in the app config?")
		return &adapters.MisconfiguredAdapter{
			TheName: "audienceNetwork",
			Err:     errors.New("Audience Network is not configured properly on this Prebid Server deploy. If you believe this should work, contact the company hosting the service and tell them to check their configuration."),
		}
	}*/

	a := &adapters.HTTPAdapter{Client: client}

	return &FacebookAdapter{
		http: a,
		URI:  "https://an.facebook.com/placementbid.ortb",
		//for AB test
		nonSecureUri: "http://an.facebook.com/placementbid.ortb",
		platformId:   "873801679416180", //platformId,
	}
}
