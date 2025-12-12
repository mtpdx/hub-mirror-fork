---
name: hub-mirror issue template
about: 用于执行 hub-mirror workflow 的 issue 模板
title: "[hub-mirror] 请求执行任务"
labels: ["hub-mirror"]
---

{
    "hub-mirror": [
        "你需要转换的镜像",
        "如果包含@sha256，会将后面的 hash 作为 tag",
        "如: nginx@sha256:q2w3e4r5t -> nginx:q2w3e4r5t",
        "每次最多 11 个",
        "改这个 json 就可以了",
        "别乱改内容",
        "标题随意，保持阵型是最好的",
        "hub-mirror 标签是必选的",
        "可通过 custom-registry 标签指定自定义 registry",
        "......"
    ],
    "custom-registry": ""
}