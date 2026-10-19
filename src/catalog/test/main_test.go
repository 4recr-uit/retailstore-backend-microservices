// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0
//
// Permission is hereby granted, free of charge, to any person obtaining a copy of this
// software and associated documentation files (the "Software"), to deal in the Software
// without restriction, including without limitation the rights to use, copy, modify,
// merge, publish, distribute, sublicense, and/or sell copies of the Software, and to
// permit persons to whom the Software is furnished to do so.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED,
// INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A
// PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT
// HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
// OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
// SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/aws-containers/retail-store-sample-app/catalog/api"
	"github.com/aws-containers/retail-store-sample-app/catalog/config"
	"github.com/aws-containers/retail-store-sample-app/catalog/controller"
	"github.com/aws-containers/retail-store-sample-app/catalog/repository"

	"github.com/gin-gonic/gin"
)

var (
	dbConfig   config.DatabaseConfiguration
	testRouter *gin.Engine
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	gin.SetMode(gin.TestMode)

	endpoint := os.Getenv("RETAIL_CATALOG_PERSISTENCE_ENDPOINT")
	if endpoint == "" {
		endpoint = os.Getenv("MYSQL_ENDPOINT")
	}

	var container testcontainers.Container
	if endpoint != "" {
		dbName := os.Getenv("RETAIL_CATALOG_PERSISTENCE_DB_NAME")
		if dbName == "" {
			dbName = "catalogdb"
		}
		dbUser := os.Getenv("RETAIL_CATALOG_PERSISTENCE_USER")
		if dbUser == "" {
			dbUser = "catalog_user"
		}
		dbPassword := os.Getenv("RETAIL_CATALOG_PERSISTENCE_PASSWORD")
		if dbPassword == "" {
			dbPassword = "unittest123"
		}
		dbConfig = config.DatabaseConfiguration{
			Type:           "mysql",
			Endpoint:       endpoint,
			Name:           dbName,
			User:           dbUser,
			Password:       dbPassword,
			ConnectTimeout: 5,
		}
	} else if os.Getenv("USE_TESTCONTAINERS") == "true" {
		var port string
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("TestContainers failed with panic: %v\n", r)
				}
			}()
			container, port, err = prepareContainer(ctx)
		}()
		if err == nil && container != nil {
			dbConfig = config.DatabaseConfiguration{
				Type:           "mysql",
				Endpoint:       fmt.Sprintf("localhost:%s", port),
				Name:           "catalogdb",
				User:           "catalog_user",
				Password:       "unittest123",
				ConnectTimeout: 5,
			}
		} else {
			log.Println("Falling back to in-memory database for tests")
			dbConfig = config.DatabaseConfiguration{
				Type: "in-memory",
			}
		}
	} else {
		dbConfig = config.DatabaseConfiguration{
			Type: "in-memory",
		}
	}

	exitCode := m.Run()

	if container != nil {
		_ = container.Terminate(ctx)
	}
	os.Exit(exitCode)
}

func router() *gin.Engine {
	if testRouter != nil {
		return testRouter
	}
	router := gin.Default()

	db, err := repository.NewRepository(dbConfig)
	if err != nil {
		log.Fatal(err)
	}

	api, err := api.NewCatalogAPI(db, nil)
	if err != nil {
		log.Fatal(err)
	}

	c, err := controller.NewController(api)
	if err != nil {
		log.Fatalln("Error creating controller", err)
	}

	catalog := router.Group("/catalog")

	catalog.GET("", c.GetProducts)

	catalog.GET("/size", c.CatalogSize)
	catalog.GET("/tags", c.ListTags)
	catalog.GET("/product/:id", c.GetProduct)

	testRouter = router
	return testRouter
}

func prepareContainer(ctx context.Context) (testcontainers.Container, string, error) {
	req := testcontainers.ContainerRequest{
		Image:        "mysql:8.0",
		ExposedPorts: []string{"3306/tcp"},
		WaitingFor:   wait.ForLog("3306"),
		Env: map[string]string{
			"MYSQL_ROOT_PASSWORD":        "unittest123",
			"MYSQL_ALLOW_EMPTY_PASSWORD": "true",
			"MYSQL_DATABASE":             "catalogdb",
			"MYSQL_USER":                 "catalog_user",
			"MYSQL_PASSWORD":             "unittest123",
		},
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, "", err
	}

	mappedPort, err := container.MappedPort(ctx, "3306")
	if err != nil {
		return nil, "", err
	}

	log.Printf("TestContainers: container %s is now running\n", req.Image)
	return container, mappedPort.Port(), nil
}

func makeRequest(method, url string, body interface{}) *httptest.ResponseRecorder {
	requestBody, _ := json.Marshal(body)
	request, _ := http.NewRequest(method, url, bytes.NewBuffer(requestBody))
	writer := httptest.NewRecorder()
	router().ServeHTTP(writer, request)
	return writer
}
