#!/bin/bash

echo "=== Git 配置设置 ==="

# 设置新的用户名和邮箱
echo "请输入新的 Git 用户名:"
read username
echo "请输入新的 Git 邮箱:"
read email

# 配置全局 Git 信息
git config --global user.name "$username"
git config --global user.email "$email"

# 也可以配置当前仓库的本地信息（可选）
echo "是否也为当前仓库设置本地配置？(y/n)"
read local_config
if [ "$local_config" = "y" ]; then
    git config user.name "$username"
    git config user.email "$email"
fi

echo "Git 用户信息已更新："
echo "用户名: $(git config user.name)"
echo "邮箱: $(git config user.email)"

echo ""
echo "=== 远程仓库检查 ==="

# 检查远程仓库
echo "当前远程仓库:"
git remote -v

echo ""
echo "如果需要更改远程仓库 URL，请使用:"
echo "git remote set-url origin https://github.com/你的用户名/你的仓库名.git"