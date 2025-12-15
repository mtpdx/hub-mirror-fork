package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"text/template"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/spf13/pflag"
)

var (
	content            = pflag.StringP("content", "", "", "原始镜像，格式为：{ \"hub-mirror\": [] }")
	maxContent         = pflag.IntP("maxContent", "", 10, "原始镜像个数限制")
	username           = pflag.StringP("username", "", "", "docker hub 用户名")
	password           = pflag.StringP("password", "", "", "docker hub 密码")
	outputPath         = pflag.StringP("outputPath", "", "output.sh", "结果输出路径")
	customRegistryPath = pflag.StringP("customRegistryPath", "", "cusreg.sh", "自定义镜像仓库结果输出路径")
	nerdctlPath        = pflag.StringP("nerdctlPath", "", "nerdctl.sh", "nerdctl 命令结果输出路径")
)

func main() {
	pflag.Parse()

	fmt.Println("验证原始镜像内容")
	var hubMirrors struct {
		Content []string `json:"hub-mirror"`
		// CustomRegistry 自定义镜像仓库
		CustomRegistry string `json:"custom-registry"`
	}
	err := json.Unmarshal([]byte(*content), &hubMirrors)
	if err != nil {
		panic(err)
	}
	if len(hubMirrors.Content) > *maxContent {
		panic("content is too long.")
	}
	fmt.Printf("%+v\n", hubMirrors)

	fmt.Println("连接 Docker")
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		panic(err)
	}

	fmt.Println("验证 Docker 用户名密码")
	if *username == "" || *password == "" {
		panic("username or password cannot be empty.")
	}
	authConfig := types.AuthConfig{
		Username: *username,
		Password: *password,
	}
	encodedJSON, err := json.Marshal(authConfig)
	if err != nil {
		panic(err)
	}
	authStr := base64.URLEncoding.EncodeToString(encodedJSON)
	_, err = cli.RegistryLogin(context.Background(), authConfig)
	if err != nil {
		panic(err)
	}

	fmt.Println("开始转换镜像")
	output := make([]struct {
		Source string
		Target string
	}, 0)

	wg := sync.WaitGroup{}

	for _, source := range hubMirrors.Content {
		if source == "" {
			continue
		}

		index := strings.Index(source, "@sha256")
		var target string
		if index != -1 {
			// 去除 @sha256，将后面的 hash 作为 tag
			cleaned := strings.Replace(source, "@sha256", "", 1)
			source = cleaned
			target = *username + "/" + strings.ReplaceAll(cleaned, "/", ".")
		} else {
			target = *username + "/" + strings.ReplaceAll(source, "/", ".")
		}

		wg.Add(1)
		go func(source, target string) {
			defer wg.Done()

			fmt.Println("开始转换", source, "=>", target)
			ctx := context.Background()

			// 拉取镜像
			pullOut, err := cli.ImagePull(ctx, source, types.ImagePullOptions{})
			if err != nil {
				panic(err)
			}
			defer pullOut.Close()
			io.Copy(os.Stdout, pullOut)

			// 重新标签
			err = cli.ImageTag(ctx, source, target)
			if err != nil {
				panic(err)
			}

			// 上传镜像
			pushOut, err := cli.ImagePush(ctx, target, types.ImagePushOptions{
				RegistryAuth: authStr,
			})
			if err != nil {
				panic(err)
			}
			defer pushOut.Close()
			io.Copy(os.Stdout, pushOut)

			output = append(output, struct {
				Source string
				Target string
			}{Source: source, Target: target})
			fmt.Println("转换成功", source, "=>", target)
		}(source, target)
	}

	wg.Wait()

	if len(output) == 0 {
		panic("output is empty.")
	}

	// 创建基础输出文件（docker pull 和 docker tag）
	outputFile, err := os.Create(*outputPath)
	if err != nil {
		panic(err)
	}
	defer outputFile.Close()

	// 基础模板：docker pull 和 docker tag
	tmpl, err := template.New("pull_images").Parse(`{{- range . -}}

docker pull {{ .Target }}
docker tag {{ .Target }} {{ .Source }}

{{ end -}}`)
	if err != nil {
		panic(err)
	}
	err = tmpl.Execute(outputFile, output)
	if err != nil {
		panic(err)
	}

	// 如果 CustomRegistry 不为空，创建自定义仓库文件
	if hubMirrors.CustomRegistry != "" {
		// 创建包含 CustomRegistry 的数据结构
		type CustomData struct {
			Output []struct {
				Source string
				Target string
			}
			CustomRegistry string
		}
		customData := CustomData{
			Output:         output,
			CustomRegistry: hubMirrors.CustomRegistry,
		}

		// 创建自定义仓库输出文件
		customRegistryFile, err := os.Create(*customRegistryPath)
		if err != nil {
			panic(err)
		}
		defer customRegistryFile.Close()

		// 自定义仓库模板
		customTmpl, err := template.New("custom_registry").Parse(`{{- range .Output -}}

docker tag {{ .Target }} {{ $.CustomRegistry }}/{{ .Source }}
docker push {{ $.CustomRegistry }}/{{ .Source }}

{{ end -}}`)
		if err != nil {
			panic(err)
		}

		// 执行自定义仓库模板
		err = customTmpl.Execute(customRegistryFile, customData)
		if err != nil {
			panic(err)
		}

		// 创建 nerdctl 输出文件
		nerdctlFile, err := os.Create(*nerdctlPath)
		if err != nil {
			panic(err)
		}
		defer nerdctlFile.Close()

		// nerdctl 模板
		nerdctlTmpl, err := template.New("nerdctl").Parse(`{{- range .Output -}}

nerdctl -n k8s.io pull {{ $.CustomRegistry }}/{{ .Source }}
nerdctl -n k8s.io tag {{ $.CustomRegistry }}/{{ .Source }} {{ .Source }}

{{ end -}}`)
		if err != nil {
			panic(err)
		}

		// 执行 nerdctl 模板
		err = nerdctlTmpl.Execute(nerdctlFile, customData)
		if err != nil {
			panic(err)
		}
	} else {
		// 创建 nerdctl 输出文件
		nerdctlFile, err := os.Create(*nerdctlPath)
		if err != nil {
			panic(err)
		}
		defer nerdctlFile.Close()
		
		// nerdctl 模板
		nerdctlTmpl, err := template.New("nerdctl").Parse(`{{- range .Output -}}

nerdctl -n k8s.io pull {{ .Target }}
nerdctl -n k8s.io tag {{ .Target }} {{ .Source }}

{{ end -}}`)
		if err != nil {
			panic(err)
		}

		// 执行 nerdctl 模板
		err = nerdctlTmpl.Execute(nerdctlFile, output)
		if err != nil {
			panic(err)
		}
	}

	fmt.Println(output)
}
