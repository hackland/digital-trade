#!/bin/bash

echo "🚀 BTC-Trader Git 配置和推送修复工具"
echo "========================================"

# 第一步：配置 Git 用户信息
echo ""
echo "🔧 步骤 1: 配置 Git 用户信息"
echo "------------------------"
echo "请输入新的 Git 用户名:"
read username
echo "请输入新的 Git 邮箱:"
read email

git config --global user.name "$username"
git config --global user.email "$email"
echo "✅ Git 用户信息已设置为: $username <$email>"

# 第二步：检查和修复分支问题
echo ""
echo "🔧 步骤 2: 检查和修复分支问题"
echo "--------------------------"

# 添加所有文件并提交（如果有未跟踪的文件）
if [[ -n $(git status --porcelain) ]]; then
    echo "发现未提交的更改，正在添加并提交..."
    git add .
    git commit -m "Update project files"
    echo "✅ 文件已提交"
fi

# 确保在 main 分支上
current_branch=$(git rev-parse --abbrev-ref HEAD)
if [ "$current_branch" != "main" ]; then
    echo "当前在 $current_branch 分支，切换到 main 分支..."
    # 如果 main 分支不存在，创建它
    if ! git show-ref --verify --quiet refs/heads/main; then
        git checkout -b main
        echo "✅ 已创建并切换到 main 分支"
    else
        git checkout main
        echo "✅ 已切换到 main 分支"
    fi
else
    echo "✅ 已在 main 分支上"
fi

# 第三步：检查远程仓库配置
echo ""
echo "🔧 步骤 3: 检查远程仓库配置"
echo "------------------------"

remote_url=$(git remote get-url origin 2>/dev/null)
if [ -z "$remote_url" ]; then
    echo "❌ 未找到远程仓库配置"
    echo "请输入 GitHub 仓库 URL (格式: https://github.com/用户名/仓库名.git):"
    read new_remote_url
    git remote add origin "$new_remote_url"
    echo "✅ 已添加远程仓库: $new_remote_url"
else
    echo "当前远程仓库: $remote_url"
    echo "是否需要更改远程仓库 URL? (y/n)"
    read change_remote
    if [ "$change_remote" = "y" ]; then
        echo "请输入新的 GitHub 仓库 URL:"
        read new_remote_url
        git remote set-url origin "$new_remote_url"
        echo "✅ 远程仓库已更新为: $new_remote_url"
    fi
fi

# 第四步：推送代码
echo ""
echo "🔧 步骤 4: 推送代码到远程仓库"
echo "--------------------------"

echo "正在推送代码到 main 分支..."
git push -u origin main

if [ $? -eq 0 ]; then
    echo "🎉 成功推送到远程仓库！"
else
    echo "❌ 推送失败，可能的原因："
    echo "1. 远程仓库不存在或权限不足"
    echo "2. 网络连接问题"
    echo "3. 需要先在 GitHub 上创建空仓库"
    echo ""
    echo "建议操作："
    echo "1. 访问 https://github.com/new 创建新仓库"
    echo "2. 确保仓库名称与本地项目匹配"
    echo "3. 重新运行此脚本"
fi

echo ""
echo "📊 当前 Git 状态:"
echo "=================="
echo "用户名: $(git config user.name)"
echo "邮箱: $(git config user.email)"
echo "当前分支: $(git rev-parse --abbrev-ref HEAD)"
echo "远程仓库: $(git remote get-url origin 2>/dev/null || echo '未配置')"
echo "最近提交: $(git log --oneline -1 2>/dev/null || echo '无提交记录')"