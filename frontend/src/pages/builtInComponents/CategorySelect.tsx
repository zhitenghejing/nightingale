import React, { useState } from 'react';
import { TreeSelect, TreeSelectProps } from 'antd';
import { Category, categoryTree } from './catalog';

export default function CategorySelect({ categories, onDropdownVisibleChange, ...props }: TreeSelectProps<string> & { categories: Category[] }) {
  const [expandedKeys, setExpandedKeys] = useState<React.Key[]>([]);

  return (
    <TreeSelect
      {...props}
      treeData={categoryTree(categories)}
      treeExpandedKeys={expandedKeys}
      onTreeExpand={setExpandedKeys}
      treeExpandAction='click'
      onDropdownVisibleChange={(open) => {
        if (open) setExpandedKeys([]);
        onDropdownVisibleChange?.(open);
      }}
    />
  );
}
