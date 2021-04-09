package ginopenapivalidator
// Copied from https://github.com/getkin/kin-openapi/blob/17153345908503543b50b7b6409f9d030bae0beb/openapi3filter/validation_error_encoder.go
// and modified
// Original license is MIT by the authors of kin-openapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
)

// Decode takes a Validation error and decodes back to a *openapi3filter.ValidationError
func Decode(err error) (*openapi3filter.ValidationError, error) {
	var cErr *openapi3filter.ValidationError
	if err.Error() == "invalid route" {
		cErr = &openapi3filter.ValidationError{
			Status: http.StatusNotFound,
			Title:  "not found",
		}
		return cErr, nil
	}
	if e, ok := err.(*routers.RouteError); ok {
		cErr = convertRouteError(e)
		return cErr, nil
	}

	e, ok := err.(*openapi3filter.RequestError)
	if !ok {
		return nil, err
	}

	if e.Err == nil {
		cErr = convertBasicRequestError(e)
	} else if e.Err == openapi3filter.ErrInvalidRequired {
		cErr = convertErrInvalidRequired(e)
	} else if innerErr, ok := e.Err.(*openapi3filter.ParseError); ok {
		cErr = convertParseError(e, innerErr)
	} else if innerErr, ok := e.Err.(*openapi3.SchemaError); ok {
		cErr = convertSchemaError(e, innerErr)
	}

	if cErr != nil {
		return cErr, nil
	}
	return nil, err
}

func convertRouteError(e *routers.RouteError) *openapi3filter.ValidationError {
	var cErr *openapi3filter.ValidationError
	fmt.Println(errors.Is(e, routers.ErrPathNotFound))
	//errors.As()
	switch e.Reason {
	case "Path doesn't support the HTTP method":
		cErr = &openapi3filter.ValidationError{Status: http.StatusMethodNotAllowed, Title: e.Reason}
	default:
		cErr = &openapi3filter.ValidationError{Status: http.StatusNotFound, Title: e.Reason}
	}
	return cErr
}

func convertBasicRequestError(e *openapi3filter.RequestError) *openapi3filter.ValidationError {
	var cErr *openapi3filter.ValidationError
	unsupportedContentType := "header 'Content-Type' has unexpected value: "
	if strings.HasPrefix(e.Reason, unsupportedContentType) {
		if strings.HasSuffix(e.Reason, `: ""`) {
			cErr = &openapi3filter.ValidationError{
				Status: http.StatusUnsupportedMediaType,
				Title:  "header 'Content-Type' is required",
			}
		} else {
			cErr = &openapi3filter.ValidationError{
				Status: http.StatusUnsupportedMediaType,
				Title:  "unsupported content type " + strings.TrimPrefix(e.Reason, unsupportedContentType),
			}
		}
	} else {
		cErr = &openapi3filter.ValidationError{
			Status: http.StatusBadRequest,
			Title:  e.Error(),
		}
	}
	return cErr
}

func convertErrInvalidRequired(e *openapi3filter.RequestError) *openapi3filter.ValidationError {
	var cErr *openapi3filter.ValidationError
	if e.Reason == openapi3filter.ErrInvalidRequired.Error() && e.Parameter != nil {
		cErr = &openapi3filter.ValidationError{
			Status: http.StatusBadRequest,
			Title:  fmt.Sprintf("Parameter '%s' in %s is required", e.Parameter.Name, e.Parameter.In),
		}
	} else {
		cErr = &openapi3filter.ValidationError{
			Status: http.StatusBadRequest,
			Title:  e.Error(),
		}
	}
	return cErr
}

func convertParseError(e *openapi3filter.RequestError, innerErr *openapi3filter.ParseError) *openapi3filter.ValidationError {
	var cErr *openapi3filter.ValidationError
	// We treat path params of the wrong type like a 404 instead of a 400
	switch {
	case innerErr.Kind == openapi3filter.KindInvalidFormat && e.Parameter != nil && e.Parameter.In == "path":
		cErr = &openapi3filter.ValidationError{
			Status: http.StatusNotFound,
			Title:  fmt.Sprintf("Resource not found with '%s' value: %v", e.Parameter.Name, innerErr.Value),
		}
	case strings.HasPrefix(innerErr.Reason, "unsupported content type"):
		cErr = &openapi3filter.ValidationError{
			Status: http.StatusUnsupportedMediaType,
			Title:  innerErr.Reason,
		}
	case innerErr.Kind == openapi3filter.KindInvalidFormat && innerErr.Reason != "":
		cErr = &openapi3filter.ValidationError{
			Status: http.StatusBadRequest,
			Title: fmt.Sprintf("Parameter '%s' in %s is invalid: %v is %s",
				e.Parameter.Name, e.Parameter.In, innerErr.Value, innerErr.Reason),
		}
	case innerErr.RootCause() != nil:
		if rootErr, ok := innerErr.Cause.(*openapi3filter.ParseError); ok &&
			rootErr.Kind == openapi3filter.KindInvalidFormat && e.Parameter.In == "query" {
			cErr = &openapi3filter.ValidationError{
				Status: http.StatusBadRequest,
				Title: fmt.Sprintf("Parameter '%s' in %s is invalid: %v is %s",
					e.Parameter.Name, e.Parameter.In, rootErr.Value, rootErr.Reason),
			}
		} else {
			cErr = &openapi3filter.ValidationError{
				Status: http.StatusBadRequest,
				Title:  innerErr.Reason,
			}
		}
	}
	if cErr.Title == "" {
		cErr.Title = "Could not parse request body"
	}
	return cErr
}

func convertSchemaError(e *openapi3filter.RequestError, innerErr *openapi3.SchemaError) *openapi3filter.ValidationError {
	cErr := &openapi3filter.ValidationError{Title: innerErr.Reason}

	// Handle "Origin" error
	if originErr, ok := innerErr.Origin.(*openapi3.SchemaError); ok {
		cErr = convertSchemaError(e, originErr)
	}

	// Add http status code
	if e.Parameter != nil {
		cErr.Status = http.StatusBadRequest
	} else if e.RequestBody != nil {
		cErr.Status = http.StatusUnprocessableEntity
	}

	// Add error source
	if e.Parameter != nil && e.Parameter.In == "query" {
		// We have a JSONPointer in the query param too so need to
		// make sure 'Parameter' check takes priority over 'Pointer'
		cErr.Source = &openapi3filter.ValidationErrorSource{
			Parameter: e.Parameter.Name,
		}
		cErr.Title += " See " + cErr.Source.Parameter
	} else if innerErr.JSONPointer() != nil {
		pointer := innerErr.JSONPointer()

		cErr.Source = &openapi3filter.ValidationErrorSource{
			Pointer: toJSONPointer(pointer),
		}
		cErr.Title += " See " + cErr.Source.Pointer
	}

	// Add details on allowed values for enums
	if innerErr.SchemaField == "enum" &&
		innerErr.Reason == "JSON value is not one of the allowed values" {
		enums := make([]string, 0, len(innerErr.Schema.Enum))
		for _, enum := range innerErr.Schema.Enum {
			enums = append(enums, fmt.Sprintf("%v", enum))
		}
		cErr.Detail = fmt.Sprintf("Value '%v' at %s must be one of: %s",
			innerErr.Value, toJSONPointer(innerErr.JSONPointer()), strings.Join(enums, ", "))
		value := fmt.Sprintf("%v", innerErr.Value)
		if (e.Parameter.Explode == nil || *e.Parameter.Explode) &&
			(e.Parameter.Style == "" || e.Parameter.Style == "form") &&
			strings.Contains(value, ",") {
			parts := strings.Split(value, ",")
			cErr.Detail = cErr.Detail + "; " + fmt.Sprintf("perhaps you intended '?%s=%s'",
				e.Parameter.Name, strings.Join(parts, "&"+e.Parameter.Name+"="))
		}
	}
	return cErr
}

func toJSONPointer(reversePath []string) string {
	return "/" + strings.Join(reversePath, "/")
}
