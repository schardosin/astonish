import { Brain } from 'lucide-react'
import OverflowNode from './OverflowNode'

export default function LlmNode({ data, selected }) {
  return (
    <OverflowNode 
      data={data} 
      selected={selected}
      icon={Brain} 
      nodeType="LLM"
      iconColor="#8b5cf6"
    />
  )
}
