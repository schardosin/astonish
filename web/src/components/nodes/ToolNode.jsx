import { Wrench } from 'lucide-react'
import OverflowNode from './OverflowNode'

export default function ToolNode({ data, selected }) {
  return (
    <OverflowNode 
      data={data} 
      selected={selected}
      icon={Wrench} 
      nodeType="Tool"
      iconColor="#7c3aed"
    />
  )
}
