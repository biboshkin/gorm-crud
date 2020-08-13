package repository

import (
	"database/sql"
	"fmt"
	"github.com/jinzhu/gorm"
	"github.com/zubroide/gorm-crud/common"
	"github.com/zubroide/gorm-crud/entity"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type ListParametersInterface interface{}

type PaginationParameters struct {
	Page      int    `form:"page,default=0" json:"page,default=0"`
	PageSize  int    `form:"page_size,default=30" json:"page_size,default=30"`
	OrderBy   string `form:"order_by,default=id" json:"order_by,default=id"`
	OrderDesc bool   `form:"order_desc,default=false" json:"order_desc,default=false"`
}

type CrudListParameters struct {
	*PaginationParameters
}

const DefaultPageSize = 30

type ListQueryBuilderInterface interface {
	ListQuery(parameters ListParametersInterface) (*gorm.DB, error)
}

type BaseListQueryBuilder struct {
	db     *gorm.DB
	logger common.LoggerInterface
	ListQueryBuilderInterface
}

func NewBaseListQueryBuilder(db *gorm.DB, logger common.LoggerInterface) ListQueryBuilderInterface {
	return &BaseListQueryBuilder{db: db, logger: logger}
}

func (c BaseListQueryBuilder) paginationQuery(parameters ListParametersInterface) *gorm.DB {
	query := c.db

	val := reflect.ValueOf(parameters).Elem()
	if val.Kind() != reflect.Struct {
		c.logger.Error("Unexpected type of parameters for paginationQuery")
		return query
	}

	paginationParameters := val.FieldByName("PaginationParameters")
	hasPaginationParams := paginationParameters.IsValid() && !paginationParameters.IsNil()

	var page int64
	page = 0
	if hasPaginationParams {
		pageValue := val.FieldByName("Page")
		if !pageValue.IsValid() || pageValue.Kind() != reflect.Int {
			c.logger.Error("Page is not specified correctly in listQuery")
		} else {
			page = pageValue.Int()
		}
	}

	var pageSize int64
	pageSize = DefaultPageSize
	if hasPaginationParams {
		pageSizeValue := val.FieldByName("PageSize")
		if !pageSizeValue.IsValid() || pageSizeValue.Kind() != reflect.Int {
			c.logger.Error("PageSize is not specified in listQuery")
		} else {
			pageSize = pageSizeValue.Int()
		}
	}

	limit := pageSize
	offset := page * pageSize
	query = query.Offset(offset).Limit(limit)

	var orderBy string
	if hasPaginationParams {
		orderByValue := val.FieldByName("OrderBy")
		if orderByValue.IsValid() && orderByValue.Kind() == reflect.String {
			orderBy = orderByValue.String()
		}
	}

	var orderDesc = false
	if hasPaginationParams {
		orderDescValue := val.FieldByName("OrderDesc")
		if orderDescValue.IsValid() && orderDescValue.Kind() == reflect.Bool {
			orderDesc = orderDescValue.Bool()
		}
	}

	if len(orderBy) > 0 {
		if orderDesc {
			query = query.Order(fmt.Sprintf("%s DESC", orderBy), true)
		} else {
			query = query.Order(fmt.Sprintf("%s ASC", orderBy), true)
		}
	}

	return query
}

func (c BaseListQueryBuilder) ListQuery(parameters ListParametersInterface) (*gorm.DB, error) {
	return c.paginationQuery(parameters), nil
}

type CrudRepositoryInterface interface {
	BaseRepositoryInterface
	GetModel() entity.InterfaceEntity
	Find(id uint) (entity.InterfaceEntity, error)
	PluckBy(fieldNames []string) (map[string]int64, error)
	ListAll() ([]entity.InterfaceEntity, error)
	List(parameters ListParametersInterface) ([]entity.InterfaceEntity, error)
	Create(item entity.InterfaceEntity) entity.InterfaceEntity
	CreateOrUpdateMany(item entity.InterfaceEntity, columns []string, values []map[string]interface{}, onConflict string) error
	Update(item entity.InterfaceEntity) entity.InterfaceEntity
	Delete(id uint) error
}

type CrudRepository struct {
	CrudRepositoryInterface
	*BaseRepository
	model            entity.InterfaceEntity // Dynamic typing
	listQueryBuilder ListQueryBuilderInterface
}

func NewCrudRepository(db *gorm.DB, model entity.InterfaceEntity, listQueryBuilder ListQueryBuilderInterface, logger common.LoggerInterface) CrudRepositoryInterface {
	repo := NewBaseRepository(db, logger).(*BaseRepository)
	return &CrudRepository{
		BaseRepository:   repo,
		model:            model,
		listQueryBuilder: listQueryBuilder,
	}
}

func (c CrudRepository) GetModel() entity.InterfaceEntity {
	return c.model
}

func (c CrudRepository) Find(id uint) (entity.InterfaceEntity, error) {
	item := reflect.New(reflect.TypeOf(c.GetModel()).Elem()).Interface()
	err := c.db.First(item, id).Error
	return item, err
}

func (c CrudRepository) PluckBy(fieldNames []string) (map[string]int64, error) {

	res := map[string]int64{}

	items, err := c.ListAll()
	if nil != err {
		return res, err
	}

	for _, item := range items {

		// build key
		values := make([]string, 0)
		val := reflect.ValueOf(item)
		for _, fieldName := range fieldNames {
			if val.FieldByName(fieldName).IsValid() {
				values = append(values, val.FieldByName(fieldName).String())
			} else {
				return res, fmt.Errorf("field with name (%s) does not exists on entity (%s)", fieldName, reflect.TypeOf(item))
			}
		}

		pluckKey := strings.Join(values, "_")

		res[pluckKey] = val.FieldByName("ID").Int()
	}

	return res, err
}

func (c CrudRepository) ListAll() ([]entity.InterfaceEntity, error) {
	entities := make([]entity.InterfaceEntity, 0)

	page := 0
	pageSize := 10000

	for {
		parameters := new(CrudListParameters)
		parameters.PaginationParameters = new(PaginationParameters)
		parameters.OrderBy = "id"
		parameters.OrderDesc = false
		parameters.PageSize = pageSize
		parameters.Page = page

		items, err := c.List(parameters)
		if nil != err {
			return entities, err
		}

		for _, item := range items {
			entities = append(entities, item)
		}

		if len(items) < pageSize {
			break
		}

		page += 1
	}

	return entities, nil
}

func (c CrudRepository) List(parameters ListParametersInterface) ([]entity.InterfaceEntity, error) {

	items := reflect.New(reflect.SliceOf(reflect.TypeOf(c.GetModel()).Elem())).Interface()
	query, err := c.listQueryBuilder.ListQuery(parameters)
	if err != nil {
		return []entity.InterfaceEntity{}, err
	}

	err = query.Find(items).Error

	entities := reflect.ValueOf(items).Elem().Interface()

	// Convert entities to slice
	var data []entity.InterfaceEntity
	sliceValue := reflect.ValueOf(entities)
	for i := 0; i < sliceValue.Len(); i++ {
		data = append(data, sliceValue.Index(i).Interface())
	}

	return data, err
}

func (c CrudRepository) Create(item entity.InterfaceEntity) entity.InterfaceEntity {
	c.db.Create(item)
	return item
}

func (c *CrudRepository) quote(str string) string {
	// postgres style escape
	str = strings.ReplaceAll(str, "'", "''")
	return fmt.Sprintf("'%s'", str)
}

func (c CrudRepository) prepareTime(val time.Time) string {
	return fmt.Sprintf("'%s'", val.Format("2006-01-02T15:04:05-0700"))
}

// CreateOrUpdateMany create or update if exists
func (c CrudRepository) CreateOrUpdateMany(
	item entity.InterfaceEntity,
	columns []string,
	values []map[string]interface{},
	onConflict string,
) error {

	if len(values) == 0 {
		return nil
	}

	var valueStrings []string
	for _, valueMap := range values {
		var valueRowString []string
		for _, column := range columns {

			colVal := valueMap[column]

			// stringify column value
			val := fmt.Sprintf("%v", colVal)

			// filter column value
			switch v := colVal.(type) {
			case sql.NullInt64:
				if !v.Valid {
					val = "NULL"
				} else {
					val = strconv.FormatInt(v.Int64, 10)
				}
			case time.Time:
				val = c.prepareTime(colVal.(time.Time))
			default:
				if reflect.TypeOf(colVal).Kind() == reflect.String {
					val = c.quote(val)
				}
			}

			valueRowString = append(valueRowString, val)
		}
		valueString := fmt.Sprintf("(%s)", strings.Join(valueRowString, ","))
		valueStrings = append(valueStrings, valueString)
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s %s",
		c.db.NewScope(item).TableName(),
		strings.Join(columns, ","),
		strings.Join(valueStrings, ","),
		onConflict)

	return c.db.Exec(query).Error
}

func (c CrudRepository) Update(item entity.InterfaceEntity) entity.InterfaceEntity {
	c.db.Save(item)
	return item
}

func (c CrudRepository) Delete(id uint) error {
	item, err := c.Find(id)
	if err != nil {
		return err
	}
	c.db.Delete(item)
	return nil
}
