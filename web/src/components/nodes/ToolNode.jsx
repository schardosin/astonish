import { Wrench } from 'lucide-react'
import OverflowNode from './OverflowNode'

export default function ToolNode({ id, data, selected }) {
  return (
    <OverflowNode 
      id={id}
      data={data} 
      selected={selected}
      icon={Wrench} 
      nodeType="Tool"
      iconColor="#7c3aed"
    />
  )
}
