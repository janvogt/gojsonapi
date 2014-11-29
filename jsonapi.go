package jsonapi

import (
	"fmt"
	"github.com/ant0ine/go-json-rest/rest"
	"net/http"
	"reflect"
	"strconv"
	"strings"
)

type UintIder interface {
	GetId() uint64
}

type StringIder interface {
	GetId() string
}

type UintGetter interface {
	UintIder
	Get(ids []uint64) (values []interface{}, err error)
}

type StringGetter interface {
	StringIder
	Get(ids []string) (values []interface{}, err error)
}

type Creater interface {
	Create(newValue interface{}) (createdValue interface{}, errs []error)
}

type UintSetter interface {
	Set(id uint64, value interface{}) (errs []error)
}

type StringSetter interface {
	Set(id string, value interface{}) (errs []error)
}

type UintDeleter interface {
	Delete(id uint64) (err error)
}

type StringDeleter interface {
	Delete(id string) (err error)
}

type Queryer interface {
	NewQuery() interface{}
	Query(query interface{}) (results []interface{}, err error)
}

type UintIncluder interface {
	IncludeUint(name string) UintGetter
}

type StringIncluder interface {
	IncludeString(name string) []StringGetter
}

func SetRoutes(resources []ResourceHandler, handler *rest.ResourceHandler) {
	var routes []*rest.Route
	for _, res := range resources {
		routes = append(routes, res.routes...)
	}
	handler.SetRoutes(routes...)
}

type ResourceHandler struct {
	routes []*rest.Route
}

func NewRessourceHandler(resource interface{}) *ResourceHandler {
	name := reflect.TypeOf(resource).Name()
	if name == "" {
		panic("Ressource must not be an unnamed type.")
	}
	name = strings.ToLower(name) + "s"
	handler := &ResourceHandler{}
	if _, ok := resource.(UintIder); ok {
		handler.setUintRoutes(name, resource)
	} else if _, ok := resource.(StringIder); ok {
		//handler.setStringRoutes(name, resource)
	} else {
		panic("Ressource must implement an Id method, that returns either uint64 or string.")
	}
	return handler
}

func (h *ResourceHandler) setUintRoutes(name string, resource interface{}) {
	if getter, suportGet := resource.(UintGetter); suportGet {
		h.routes = append(h.routes, &rest.Route{"GET", "/" + name + "#ids", makeUintGetHandler(name, getter, makeIncluder(resource))})
	}
}

func makeUintGetHandler(name string, getter UintGetter, inc includer) rest.HandlerFunc {
	return func(rw rest.ResponseWriter, req *rest.Request) {
		defer respondWithErrors(rw)
		ids, err := splitUintIds(req.PathParam("ids"))
		if err != nil {
			rw.WriteHeader(http.StatusBadRequest)
			panic(fmt.Errorf("Invalid request: %s", req.RequestURI))
		}
		resp := make(map[string]interface{})
		resources, err := getter.Get(ids)
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			panic(err)
		}
		if len(resources) == 1 {
			resp[name] = resources[0]
		} else {
			resp[name] = resources
		}
		l := inc(resources, rw, req)
		if l != 0 {
			resp["linked"] = l.Linked
			resp["links"] = l.Links
		}
		rw.WriteJson(resp)
	}
}

// splitUintIds splits a given comma seperated string to an array of integers
func splitUintIds(idString string) (ids []uint64, err error) {
	idStrs := strings.Split(idString, ",")
	ids = make([]uint64, len(idStrs))
	for i, idStr := range idStrs {
		ids[i], err = strconv.ParseUint(idStr, 0, 64)
		if err != nil {
			return
		}
	}
	return
}

// splitStringIds splits a given comma seperated string to an array of strings
func splitStringIds(idString string) ([]string, error) {
	return strings.Split(idString, ",")
}

// type linked struct {
// 	Linked map[string][]interface{}
// 	Links  map[string][]
// }

type include struct {
	RelName string
	RelDesc struct {
		Href string `json:href,omitempty`
		Type string `json:type`
	}
	Resources []interface{}
}

type includer func([]interface{}, rest.ResponseWriter, *rest.Request) *linked

type errors struct {
	Errors []interface{} `json:errors`
}

func respondWithErrors(rw rest.ResponseWriter) {
	err := recover()
	if err == nil {
		return
	}
	var resp errors
	if errs, isSlice := err.([]interface{}); isSlice {
		resp = errors{errs}
	} else {
		errs := make([]interface{}, 1)
		errs[0] = err
		resp = errors{errs}
	}
	rw.WriteJson(resp)
}

func makeIncluders(resource interface{}) (includers map[string]includer) {
	includes := make(map[string]includer)
	if inc, hasUintIncludes := resource.(UintIncluder); hasUintIncludes {
		uintToOne, uintToMany = getUintReferences(includer)
	}
	if inc, hasStringIncludes := resource.(StringIncluder); hasStringIncludes {
		stringToOne, stringToMany = getUintReferences(includer)
	}
	return
}

// getLinksStruct gets the links field of the resource and ensures it's a struct. Panics on failiure.
func getLinksStruct(resource interface{}) reflect.Type {
	t := reflect.TypeOf(resource)
	linksField, ok := t.FieldByName("links")
	if !ok || linksField.Type.Kind() != reflect.Struct {
		panic(fmt.Errorf("%s is an UintIncluder, but has no links field containing a struct of references.", reflect.TypeOf(resource).Name()))
	}
	return linksField.Type
}

// getUintIncluders gets all references for the given UintIncluder. Panics if there are none.
func getUintIncluders(resource UintIncluder, includers *map[string]includer) {
	l := getLinksStruct(resource)
	idExtractors := make(map[string]func(res interface{}, idMap map[uint64]bool))
	for i := 0; i < l.NumField(); i++ {
		inc := l.Field(i).Type
		if inc.ConvertibleTo(reflect.Uint64) {
			name := strings.ToLower(inc.Name)
			index := l.Field.Index
			idExtractors[name] = func(res interface{}, idMap map[uint64]bool) {
				idMap[reflect.TypeOf(resource[i]).FieldByIndex(index).(uint64)] = true
			}
			toOne[strings.ToLower(inc.Name)] = l.Field.Index
		} else if inc.Kind == reflect.Slice && SliceOf(inc).ConvertibleTo(reflect.Uint64) {
			name := strings.ToLower(inc.Name)
			index := l.Field.Index
			idExtractors[name] = func(res interface{}, idMap map[uint64]bool) {
				ids := reflect.TypeOf(resource[i]).FieldByIndex(index).([]uint64)
				for _, id := range ids {
					idMap[id] = true
				}
			}
		}
	}
	if len(idExtractors) == 0 {
		panic(fmt.Errorf("%s is an StringIncluder, but it's links struct does not contain any string references.", reflect.TypeOf(resource).Name()))
	}
	for name, idExtractor := range idExtractors {
		dest := resource.IncludeUint(name)
		includers[name] = makeIncluder(name, resource, dest, makeUintResourceGetter(dest, idExtractor))
	}
	return
}

func makeUintResourceGetter(dest UintGetter, idExtractor func(interface{}, map[uint64]bool)) (res []interface{}, err error) {
	idMap := make(map[uint64]bool)
	for i := range resources {
		idExtractor(resources[i], idMap)
	}
	ids := make([]uint64, len(idMap))
	var i int64
	for id := range idMap {
		ids[i] = id
		i++
	}
	res, err = dest.Get(ids)
	return
}

// getStringIncluders gets all references for the given StringIncluder. Panics if there are none.
func getStringIncluders(resource StringIncluder, includers *map[string]includer) {
	l := getLinksStruct(resource)
	for i := 0; i < l.NumField(); i++ {
		inc := l.Field(i).Type
		if inc.Kind == reflect.String {
			toOne[strings.ToLower(inc.Name)] = l.Field.Index
		} else if inc.Kind == reflect.Slice && SliceOf(inc).Kind() == reflect.String {
			toMany[strings.ToLower(inc.Name)] = l.Field.Index
		}
	}
	if len(toOne)+len(toMany) == 0 {
		panic(fmt.Errorf("%s is an StringIncluder, but it's links struct does not contain any string references.", reflect.TypeOf(resource).Name()))
	}
	return
}

func makeIncluder(name string, source, dest interface{}, getter func([]interface{}) ([]interface{}, err)) {
	includeStruct := include{RelName: resourceName(source) + "." + name}
	includeStruct.RelDesc.Type = resourceName(dest)
	return func(res []interface{}, rw rest.ResponseWriter, req *rest.Request) *include {
		inc := includeStruct
		var err error
		inc.Resources, err = getter(res)
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			panic(err)
		}
	}
}

func resourceName(res interface{}) string {
	strings.ToLower(reflect.TypeOf(res).Name()) + "s"
}
