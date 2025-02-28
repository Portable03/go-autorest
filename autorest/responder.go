package autorest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Responder is the interface that wraps the Respond method.
//
// Respond accepts and reacts to an http.Response. Implementations must ensure to not share or hold
// state since Responders may be shared and re-used.
type Responder interface {
	Respond(*http.Response) error
}

// ResponderFunc is a method that implements the Responder interface.
type ResponderFunc func(*http.Response) error

// Respond implements the Responder interface on ResponderFunc.
func (rf ResponderFunc) Respond(r *http.Response) error {
	return rf(r)
}

// RespondDecorator takes and possibly decorates, by wrapping, a Responder. Decorators may react to
// the http.Response and pass it along or, first, pass the http.Response along then react.
type RespondDecorator func(Responder) Responder

// CreateResponder creates, decorates, and returns a Responder. Without decorators, the returned
// Responder returns the passed http.Response unmodified. Responders may or may not be safe to share
// and re-used: It depends on the applied decorators. For example, a standard decorator that closes
// the response body is fine to share whereas a decorator that reads the body into a passed struct
// is not.
//
// To prevent memory leaks, ensure that at least one Responder closes the response body.
func CreateResponder(decorators ...RespondDecorator) Responder {
	return DecorateResponder(
		Responder(ResponderFunc(func(r *http.Response) error { return nil })),
		decorators...)
}

// DecorateResponder accepts a Responder and a, possibly empty, set of RespondDecorators, which it
// applies to the Responder. Decorators are applied in the order received, but their affect upon the
// request depends on whether they are a pre-decorator (react to the http.Response and then pass it
// along) or a post-decorator (pass the http.Response along and then react).
func DecorateResponder(r Responder, decorators ...RespondDecorator) Responder {
	for _, decorate := range decorators {
		r = decorate(r)
	}
	return r
}

// Respond accepts an http.Response and a, possibly empty, set of RespondDecorators.
// It creates a Responder from the decorators it then applies to the passed http.Response.
func Respond(r *http.Response, decorators ...RespondDecorator) error {
	if r == nil {
		return nil
	}
	return CreateResponder(decorators...).Respond(r)
}

// ByIgnoring returns a RespondDecorator that ignores the passed http.Response passing it unexamined
// to the next RespondDecorator.
func ByIgnoring() RespondDecorator {
	return func(r Responder) Responder {
		return ResponderFunc(func(resp *http.Response) error {
			return r.Respond(resp)
		})
	}
}

// ByClosing returns a RespondDecorator that first invokes the passed Responder after which it
// closes the response body. Since the passed Responder is invoked prior to closing the response
// body, the decorator may occur anywhere within the set.
func ByClosing() RespondDecorator {
	return func(r Responder) Responder {
		return ResponderFunc(func(resp *http.Response) error {
			err := r.Respond(resp)
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
			return err
		})
	}
}

// ByClosingIfError returns a RespondDecorator that first invokes the passed Responder after which
// it closes the response if the passed Responder returns an error and the response body exists.
func ByClosingIfError() RespondDecorator {
	return func(r Responder) Responder {
		return ResponderFunc(func(resp *http.Response) error {
			err := r.Respond(resp)
			if err != nil && resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
			return err
		})
	}
}

// ByUnmarshallingJSON returns a RespondDecorator that decodes a JSON document returned in the
// response Body into the value pointed to by v.
func ByUnmarshallingJSON(v interface{}) RespondDecorator {
	return func(r Responder) Responder {
		return ResponderFunc(func(resp *http.Response) error {
			err := r.Respond(resp)
			if err == nil {
				b := bytes.Buffer{}
				d := json.NewDecoder(io.TeeReader(resp.Body, &b))
				err = d.Decode(v)
				if err != nil {
					err = fmt.Errorf("Error (%v) occurred decoding JSON (\"%s\")", err, b.String())
				}
			}
			return err
		})
	}
}

// WithErrorUnlessStatusCode returns a RespondDecorator that emits an error unless the response
// StatusCode is among the set passed. Since these are artificial errors, the response body
// may still require closing.
func WithErrorUnlessStatusCode(codes ...int) RespondDecorator {
	return func(r Responder) Responder {
		return ResponderFunc(func(resp *http.Response) error {
			err := r.Respond(resp)
			if err == nil && !ResponseHasStatusCode(resp, codes...) {
				err = NewErrorWithStatusCode("autorest", "WithErrorUnlessStatusCode", resp.StatusCode, "%v %v failed with %s",
					resp.Request.Method,
					resp.Request.URL,
					resp.Status)
			}
			return err
		})
	}
}

// WithErrorUnlessOK returns a RespondDecorator that emits an error if the response StatusCode is
// anything other than HTTP 200.
func WithErrorUnlessOK() RespondDecorator {
	return WithErrorUnlessStatusCode(http.StatusOK)
}

// ExtractHeader extracts all values of the specified header from the http.Response. It returns an
// empty string slice if the passed http.Response is nil or the header does not exist.
func ExtractHeader(header string, resp *http.Response) []string {
	if resp != nil && resp.Header != nil {
		return resp.Header[http.CanonicalHeaderKey(header)]
	}
	return nil
}

// ExtractHeaderValue extracts the first value of the specified header from the http.Response. It
// returns an empty string if the passed http.Response is nil or the header does not exist.
func ExtractHeaderValue(header string, resp *http.Response) string {
	h := ExtractHeader(header, resp)
	if len(h) > 0 {
		return h[0]
	}
	return ""
}
