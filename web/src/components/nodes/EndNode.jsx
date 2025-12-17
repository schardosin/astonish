import { Square } from 'lucide-react'
import OverflowNode from './OverflowNode'

export default function EndNode({ data, selected }) {
  return (
    <OverflowNode 
      data={data} 
      selected={selected}
      icon={Square} 
      nodeType="End"
      iconColor="#ef4444"
      hasBottomHandle={false}
    />
  )
}
