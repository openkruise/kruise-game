package server

import (
	"log"

	"github.com/gin-gonic/gin"
	gameV1alpha1 "github.com/openkruise/kruise-game/pkg/client/clientset/versioned/typed/apis/v1alpha1"
	"k8s.io/client-go/tools/clientcmd"
)

type gameServerUpdateRequest struct {
	gameServerIds []int
	imageTag      string
}

func StartServer() {

	//loads kubeConfig
	kubeConfig := "~/.kube/config"
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfig)
	if err != nil {
		log.Fatalf("failed to create config: %v", err)
	}

	//creates gameV1alpha client from Config
	client, err := gameV1alpha1.NewForConfig(config)
	if err != nil {
		log.Fatalf("failed to creat gamev1alpha client: %v", err)
	}

	s := gin.Default()
	s.PUT("/gamerserver/update/")

	log.Println("starting server on port : 51322 ")
	err = s.Run(":51322")
	if err != nil {
		log.Fatalf("failed to start server: %v", err)
	}

}

func updateImageTagHandler(client *gameV1alpha1.GameV1alpha1Client, c *gin.Context) {

}
func changeUpdatePriority(client *gameV1alpha1.GameV1alpha1Client, c *gin.Context) {

}
