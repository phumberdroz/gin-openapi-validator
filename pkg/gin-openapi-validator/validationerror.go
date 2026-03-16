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

const unsupportedContentTypeReason = "header 'Content-Type' has unexpected value: "

type ValidationPhase string

const (
	ValidationPhaseRequest  ValidationPhase = "request"
	ValidationPhaseResponse ValidationPhase = "response"
)

type ValidationKind string

const (
	ValidationKindUnknown  ValidationKind = "unknown"
	ValidationKindRoute    ValidationKind = "route"
	ValidationKindRequest  ValidationKind = "request"
	ValidationKindResponse ValidationKind = "response"
	ValidationKindParse    ValidationKind = "parse"
	ValidationKindSchema   ValidationKind = "schema"
)

// ContractError wraps a request or response validation error with both the normalized
// validation payload and the underlying kin-openapi details.
type ContractError struct {
	Phase         ValidationPhase
	Kind          ValidationKind
	Status        int
	Title         string
	Detail        string
	Source        *openapi3filter.ValidationErrorSource
	Reason        string
	Validation    *openapi3filter.ValidationError
	RequestError  *openapi3filter.RequestError
	ResponseError *openapi3filter.ResponseError
	RouteError    *routers.RouteError
	ParseError    *openapi3filter.ParseError
	SchemaError   *openapi3.SchemaError
	Parameter     *openapi3.Parameter
	RequestBody   *openapi3.RequestBody
	ResponseInput *openapi3filter.ResponseValidationInput
	Err           error
}

func (e *ContractError) Error() string {
	if e == nil {
		return ""
	}

	if e.Validation != nil {
		return e.Validation.Error()
	}

	if e.Err != nil {
		return e.Err.Error()
	}

	return string(e.Kind) + " validation error"
}

func (e *ContractError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}

func newContractError(phase ValidationPhase, err error) *ContractError {
	contractErr := &ContractError{
		Phase: phase,
		Kind:  ValidationKindUnknown,
		Err:   err,
	}

	if err == nil {
		return contractErr
	}

	enrichContractError(contractErr)

	decoded, decodeErr := decodeValidationError(err)
	if decodeErr == nil && decoded != nil {
		applyDecodedValidation(contractErr, decoded)
		return contractErr
	}

	if phase == ValidationPhaseResponse {
		applyDecodedValidation(contractErr, &openapi3filter.ValidationError{
			Status: http.StatusInternalServerError,
			Title:  "Response body does not conform to the OpenAPI specification",
			Detail: err.Error(),
		})
	}

	return contractErr
}

func enrichContractError(contractErr *ContractError) {
	enrichRouteError(contractErr)
	enrichRequestError(contractErr)
	enrichResponseError(contractErr)
	enrichParseError(contractErr)
	enrichSchemaError(contractErr)
}

func enrichRouteError(contractErr *ContractError) {
	var routeErr *routers.RouteError
	if errors.As(contractErr.Err, &routeErr) {
		contractErr.Kind = ValidationKindRoute
		contractErr.RouteError = routeErr
		contractErr.Reason = routeErr.Reason
	}
}

func enrichRequestError(contractErr *ContractError) {
	var requestErr *openapi3filter.RequestError
	if errors.As(contractErr.Err, &requestErr) {
		contractErr.Kind = ValidationKindRequest
		contractErr.RequestError = requestErr
		contractErr.Reason = requestErr.Reason
		contractErr.Parameter = requestErr.Parameter
		contractErr.RequestBody = requestErr.RequestBody
	}
}

func enrichResponseError(contractErr *ContractError) {
	var responseErr *openapi3filter.ResponseError
	if errors.As(contractErr.Err, &responseErr) {
		contractErr.Kind = ValidationKindResponse
		contractErr.ResponseError = responseErr
		contractErr.Reason = responseErr.Reason
		contractErr.ResponseInput = responseErr.Input
	}
}

func enrichParseError(contractErr *ContractError) {
	var parseErr *openapi3filter.ParseError
	if errors.As(contractErr.Err, &parseErr) {
		contractErr.Kind = ValidationKindParse
		contractErr.ParseError = parseErr
	}
}

func enrichSchemaError(contractErr *ContractError) {
	var schemaErr *openapi3.SchemaError
	if errors.As(contractErr.Err, &schemaErr) {
		contractErr.Kind = ValidationKindSchema
		contractErr.SchemaError = schemaErr
	}
}

func applyDecodedValidation(contractErr *ContractError, decoded *openapi3filter.ValidationError) {
	contractErr.Validation = decoded
	contractErr.Status = decoded.Status
	contractErr.Title = decoded.Title
	contractErr.Detail = decoded.Detail
	contractErr.Source = decoded.Source
}

// Decode takes a validation error and decodes it back to an *openapi3filter.ValidationError.
func Decode(err error) (*openapi3filter.ValidationError, error) {
	var contractErr *ContractError
	if errors.As(err, &contractErr) {
		if contractErr.Validation != nil {
			return contractErr.Validation, nil
		}

		if contractErr.Err != nil && contractErr.Err != err {
			return decodeValidationError(contractErr.Err)
		}
	}

	return decodeValidationError(err)
}

func decodeValidationError(err error) (*openapi3filter.ValidationError, error) {
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
	switch e.Reason {
	case "Path doesn't support the HTTP method":
		return &openapi3filter.ValidationError{Status: http.StatusMethodNotAllowed, Title: e.Reason}
	default:
		return &openapi3filter.ValidationError{Status: http.StatusNotFound, Title: e.Reason}
	}
}

func convertBasicRequestError(e *openapi3filter.RequestError) *openapi3filter.ValidationError {
	if strings.HasPrefix(e.Reason, unsupportedContentTypeReason) {
		if strings.HasSuffix(e.Reason, `: ""`) {
			return &openapi3filter.ValidationError{
				Status: http.StatusUnsupportedMediaType,
				Title:  "header 'Content-Type' is required",
			}
		}

		return &openapi3filter.ValidationError{
			Status: http.StatusUnsupportedMediaType,
			Title:  "unsupported content type " + strings.TrimPrefix(e.Reason, unsupportedContentTypeReason),
		}
	}

	return &openapi3filter.ValidationError{
		Status: http.StatusBadRequest,
		Title:  e.Error(),
	}
}

func convertErrInvalidRequired(e *openapi3filter.RequestError) *openapi3filter.ValidationError {
	if e.Reason == openapi3filter.ErrInvalidRequired.Error() && e.Parameter != nil {
		return &openapi3filter.ValidationError{
			Status: http.StatusBadRequest,
			Title:  fmt.Sprintf("Parameter '%s' in %s is required", e.Parameter.Name, e.Parameter.In),
		}
	}

	return &openapi3filter.ValidationError{
		Status: http.StatusBadRequest,
		Title:  e.Error(),
	}
}

func convertParseError(e *openapi3filter.RequestError, innerErr *openapi3filter.ParseError) *openapi3filter.ValidationError {
	var cErr *openapi3filter.ValidationError

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

	if originErr, ok := innerErr.Origin.(*openapi3.SchemaError); ok {
		cErr = convertSchemaError(e, originErr)
	}

	if e.Parameter != nil {
		cErr.Status = http.StatusBadRequest
	} else if e.RequestBody != nil {
		cErr.Status = http.StatusUnprocessableEntity
	}

	if e.Parameter != nil && e.Parameter.In == "query" {
		cErr.Source = &openapi3filter.ValidationErrorSource{
			Parameter: e.Parameter.Name,
		}

		cErr.Title += " See " + cErr.Source.Parameter
	} else if innerErr.JSONPointer() != nil {
		cErr.Source = &openapi3filter.ValidationErrorSource{
			Pointer: toJSONPointer(innerErr.JSONPointer()),
		}

		cErr.Title += " See " + cErr.Source.Pointer
	}

	if innerErr.SchemaField == "type" &&
		strings.Contains(strings.ToLower(innerErr.Reason), "integer") &&
		cErr.Source != nil && cErr.Source.Pointer != "" {
		cErr.Title = fmt.Sprintf("Field must be set to integer or not be present See %s", cErr.Source.Pointer)
	}

	if innerErr.SchemaField == "enum" &&
		innerErr.Reason == "JSON value is not one of the allowed values" {
		enums := make([]string, 0, len(innerErr.Schema.Enum))
		for _, enum := range innerErr.Schema.Enum {
			enums = append(enums, fmt.Sprintf("%v", enum))
		}

		cErr.Detail = fmt.Sprintf("Value '%v' at %s must be one of: %s",
			innerErr.Value, toJSONPointer(innerErr.JSONPointer()), strings.Join(enums, ", "))

		value := fmt.Sprintf("%v", innerErr.Value)
		if e.Parameter != nil &&
			(e.Parameter.Explode == nil || *e.Parameter.Explode) &&
			(e.Parameter.Style == "" || e.Parameter.Style == "form") &&
			strings.Contains(value, ",") {
			parts := strings.Split(value, ",")

			cErr.Detail += "; " + fmt.Sprintf("perhaps you intended '?%s=%s'",
				e.Parameter.Name, strings.Join(parts, "&"+e.Parameter.Name+"="))
		}
	}

	return cErr
}

func toJSONPointer(reversePath []string) string {
	return "/" + strings.Join(reversePath, "/")
}
