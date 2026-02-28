#!/bin/bash

echo "=== Git 分支问题诊断和修复 ==="

# 检查当前分支状态
echo "当前分支状态:"
git status

echo ""
echo "所有本地分支:"
git branch

echo ""
echo "所有远程分支:"
git branch -r

echo ""
echo "检查 HEAD 状态:"
git log --oneline -5

# 如果没有提交历史，创建初始提交
if [ -z "$(git log --oneline -1 2>/dev/null)" ]; then
    echo ""
    echo "检测到没有提交历史，创建初始提交..."
    git add .
    git commit -m "Initial commit"
fi

# 检查是否有 main 分支
if ! git show-ref --verify --quiet refs/heads/main; then
    echo ""
    echo "没有找到 main 分支，创建 main 分支..."
    git checkout -b main
else
    echo ""
    echo "切换到 main 分支..."
    git checkout main
fi

# 设置上游分支
echo ""
echo "设置上游分支..."
git push -u origin main

echo ""
echo "=== 完成 ==="
echo "如果仍有问题，请检查远程仓库是否存在以及权限设置"