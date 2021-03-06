/*** Copyright (c) 2016, University of Florida Research Foundation, Inc. ***
 *** For more information please refer to the LICENSE.md file            ***/

package gorods

// #include "wrapper.h"
import "C"

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"strings"
	"unsafe"
)

// collection.DataObjs()     -> type: DataObjs
// collection.Collections()  -> type: Collections
// collection.All()          -> type: []interface{}
// collection.Both()         -> (type: DataObjs, type: Collections)
type Collection struct {
	Path        string
	Name        string
	DataObjects []interface{}
	Con         *Connection
	Col         *Collection
	Recursive   bool

	chandle C.int
}

// collections.Find(relPath) -> type: Collection
type Collections []*Collection

func (colls Collections) Exists(path string) bool {
	if c := colls.Find(path); c != nil {
		return true
	}

	return false
}

func (colls Collections) Find(path string) *Collection {
	if path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}

	for i, col := range colls {
		if col.Path == path || col.Name == path {
			return colls[i]
		}
	}

	return nil
}

func (colls Collections) FindRecursive(path string) *Collection {
	if path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}

	for i, col := range colls {
		if col.Path == path || col.Name == path {
			return colls[i]
		}

		if col.Recursive {
			// Use Collections() since we already loaded everything
			if subCol := col.Collections().FindRecursive(path); subCol != nil {
				return subCol
			}
		} else {
			// Use DataObjects so we don't load new collections
			var filtered Collections

			for n, obj := range col.DataObjects {
				if reflect.TypeOf(obj).String() == "*gorods.Collection" {
					filtered = append(filtered, col.DataObjects[n].(*Collection))
				}
			}

			if subCol := filtered.FindRecursive(path); subCol != nil {
				return subCol
			}
		}
	}

	return nil
}

func (obj *Collection) String() string {
	str := fmt.Sprintf("Collection: %v\n", obj.Path)

	for _, o := range obj.DataObjs() {
		str += fmt.Sprintf("\tD: %v\n", o.Name)
	}

	for _, o := range obj.Collections() {
		str += fmt.Sprintf("\tC: %v\n", o.Name)
	}

	return str
}

// Init from *C.collEnt_t
func NewCollection(data *C.collEnt_t, acol *Collection) *Collection {

	col := new(Collection)

	col.Col = acol
	col.Con = col.Col.Con
	col.Path = C.GoString(data.collName)

	pathSlice := strings.Split(col.Path, "/")

	col.Name = pathSlice[len(pathSlice)-1]

	if acol.Recursive {
		col.Recursive = true
		col.Init()
	}

	return col
}

// Called from connection
func GetCollection(startPath string, recursive bool, con *Connection) *Collection {
	col := new(Collection)

	col.Con = con
	col.Path = startPath
	col.Recursive = recursive

	if col.Path[len(col.Path)-1] == '/' {
		col.Path = col.Path[:len(col.Path)-1]
	}

	pathSlice := strings.Split(col.Path, "/")
	col.Name = pathSlice[len(pathSlice)-1]

	if col.Recursive {
		col.Init()
	}

	return col
}

func (col *Collection) Init() *Collection {
	// If connection hasn't been opened, do it!
	if int(col.chandle) == 0 {
		col.Open()
		col.ReadCollection()
	}

	return col
}

// Opens connection to collection, passes in flag options
func (col *Collection) Open() *Collection {
	var errMsg *C.char

	path := C.CString(col.Path)

	defer C.free(unsafe.Pointer(path))

	if status := C.gorods_open_collection(path, &col.chandle, col.Con.ccon, &errMsg); status != 0 {
		panic(fmt.Sprintf("iRods Open Collection Failed: %v, %v", col.Path, C.GoString(errMsg)))
	}

	return col
}

func (col *Collection) Close() *Collection {
	var errMsg *C.char

	if status := C.gorods_close_collection(col.chandle, col.Con.ccon, &errMsg); status != 0 {
		panic(fmt.Sprintf("iRods Close Collection Failed: %v, %v", col.Path, C.GoString(errMsg)))
	}

	col.chandle = C.int(0)

	return col
}

// Reads data into col.DataObjects
func (col *Collection) ReadCollection() {

	// Init C varaibles
	var (
		err     *C.char
		arr     *C.collEnt_t
		arrSize C.int
	)

	// Read data objs from collection
	C.gorods_read_collection(col.Con.ccon, col.chandle, &arr, &arrSize, &err)

	// Get result length
	arrLen := int(arrSize)

	unsafeArr := unsafe.Pointer(arr)
	defer C.free(unsafeArr)

	// Convert C array to slice, backed by arr *C.collEnt_t
	slice := (*[1 << 30]C.collEnt_t)(unsafeArr)[:arrLen:arrLen]

	col.DataObjects = make([]interface{}, 0)

	for i, _ := range slice {
		obj := &slice[i]

		isCollection := (obj.objType != C.DATA_OBJ_T)

		if isCollection {
			col.DataObjects = append(col.DataObjects, NewCollection(obj, col))
		} else {
			col.DataObjects = append(col.DataObjects, NewDataObj(obj, col))

			// Strings only in DataObj types
			C.free(unsafe.Pointer(obj.dataName))
			C.free(unsafe.Pointer(obj.dataId))
			C.free(unsafe.Pointer(obj.chksum))
			C.free(unsafe.Pointer(obj.dataType))
			C.free(unsafe.Pointer(obj.resource))
			C.free(unsafe.Pointer(obj.rescGrp))
			C.free(unsafe.Pointer(obj.phyPath))
		}

		// String in both object types
		C.free(unsafe.Pointer(obj.ownerName))
		C.free(unsafe.Pointer(obj.collName))
		C.free(unsafe.Pointer(obj.createTime))
		C.free(unsafe.Pointer(obj.modifyTime))

	}

	col.Close()
}

func (col *Collection) DataObjs() DataObjs {
	col.Init()

	var response DataObjs

	for i, obj := range col.DataObjects {
		if reflect.TypeOf(obj).String() == "*gorods.DataObj" {
			response = append(response, col.DataObjects[i].(*DataObj))
		}
	}

	return response
}

func (col *Collection) Collections() Collections {
	col.Init()

	var response Collections

	for i, obj := range col.DataObjects {
		if reflect.TypeOf(obj).String() == "*gorods.Collection" {
			response = append(response, col.DataObjects[i].(*Collection))
		}
	}

	return response
}

func (col *Collection) Put(localFile string) *DataObj {
	col.Init()

	data, err := ioutil.ReadFile(localFile)
	if err != nil {
		panic(fmt.Sprintf("Can't read file for Put(): %v", localFile))
	}

	fileName := filepath.Base(localFile)

	newFile := CreateDataObj(DataObjOptions{
		Name:  fileName,
		Size:  int64(len(data)),
		Mode:  0750,
		Force: true,
	}, col)

	newFile.Write(data)

	return newFile
}

func (col *Collection) Add(dataObj interface{}) *Collection {
	col.Init()

	col.DataObjects = append(col.DataObjects, dataObj)

	return col
}

func (col *Collection) All() []interface{} {
	col.Init()

	return col.DataObjects
}

func (col *Collection) Both() (DataObjs, Collections) {
	return col.DataObjs(), col.Collections()
}

func (col *Collection) Exists(path string) bool {
	return col.DataObjs().Exists(path) || col.Collections().Exists(path)
}

func (col *Collection) Find(path string) interface{} {
	if d := col.DataObjs().Find(path); d != nil {
		return d
	}

	if c := col.Collections().Find(path); c != nil {
		return c
	}

	return nil
}

func (col *Collection) Cd(path string) *Collection {
	return col.Collections().Find(path)
}

func (col *Collection) Get(path string) *DataObj {
	return col.DataObjs().Find(path)
}
